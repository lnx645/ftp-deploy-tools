package main

import (
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"
	"github.com/schollz/progressbar/v3"
	"golang.org/x/term"
)

type DeployClient struct {
	cfg    *Config
	conn   *ftp.ServerConn
}

func Connect(cfg *Config) (*DeployClient, error) {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	var opts []ftp.DialOption
	if cfg.PassiveMode {
		opts = append(opts, ftp.DialWithTimeout(15*time.Second))
	} else {
		opts = append(opts, ftp.DialWithTimeout(15*time.Second), ftp.DialWithDisabledEPSV(true))
	}
	conn, err := ftp.Dial(addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("konek gagal: %w", err)
	}
	password := cfg.Password
	if env := os.Getenv("FTPDEPLOY_PASSWORD"); env != "" {
		password = env
	}
	if err := conn.Login(cfg.Username, password); err != nil {
		conn.Quit()
		return nil, fmt.Errorf("login gagal: %w", err)
	}
	return &DeployClient{cfg: cfg, conn: conn}, nil
}

func (c *DeployClient) Close() {
	if c.conn != nil {
		c.conn.Quit()
	}
}

type RemoteFile struct {
	RelPath string
	Size    int64
	Time    time.Time
}

// remoteAbs builds the absolute remote path for a relative path.
func (c *DeployClient) remoteAbs(rel string) string {
	base := strings.TrimRight(c.cfg.RemoteDir, "/")
	if base == "" {
		base = "/"
	}
	if strings.HasPrefix(rel, "/") {
		return rel
	}
	if base == "/" {
		return "/" + rel
	}
	return base + "/" + rel
}

// ListRemote walks the remote tree recursively, returning files only.
func (c *DeployClient) ListRemote() ([]*RemoteFile, error) {
	var out []*RemoteFile
	err := c.walkRemote(c.cfg.RemoteDir, "", &out)
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RelPath < out[j].RelPath })
	return out, nil
}

func (c *DeployClient) walkRemote(dir, prefix string, out *[]*RemoteFile) error {
	entries, err := c.conn.List(dir)
	if err != nil {
		// Directory missing on server.
		return nil
	}
	for _, e := range entries {
		name := e.Name
		if name == "." || name == ".." {
			continue
		}
		rel := name
		if prefix != "" {
			rel = prefix + "/" + name
		}
		// Ignore patterns apply on both sides (git-like).
		if c.cfg.IsIgnored(rel) {
			continue
		}
		switch e.Type {
		case ftp.EntryTypeFolder:
			if err := c.walkRemote(c.remoteAbs(rel), rel, out); err != nil {
				return err
			}
		case ftp.EntryTypeFile, ftp.EntryTypeLink:
			t := e.Time
			if t.IsZero() {
				t = time.Now()
			}
			*out = append(*out, &RemoteFile{RelPath: rel, Size: int64(e.Size), Time: t})
		}
	}
	return nil
}

// ListRemoteDirs lists files (non-recursive) under the given relative
// directories. Used right after an upload to capture the server's size/mtime
// without re-walking the whole tree.
func (c *DeployClient) ListRemoteDirs(dirs []string) ([]*RemoteFile, error) {
	var out []*RemoteFile
	seen := map[string]bool{}
	for _, dir := range dirs {
		dir = strings.Trim(dir, "/")
		if dir == "." {
			dir = ""
		}
		if seen[dir] {
			continue
		}
		seen[dir] = true
		entries, err := c.conn.List(c.remoteAbs(dir))
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.Type == ftp.EntryTypeFolder {
				continue
			}
			rel := e.Name
			if dir != "" {
				rel = dir + "/" + e.Name
			}
			if c.cfg.IsIgnored(rel) {
				continue
			}
			t := e.Time
			if t.IsZero() {
				t = time.Now()
			}
			out = append(out, &RemoteFile{RelPath: rel, Size: int64(e.Size), Time: t})
		}
	}
	return out, nil
}

// EnsureRemoteDir creates remote directories for the parent of relPath.
var createdDirs = map[string]bool{}

func (c *DeployClient) EnsureRemoteDir(rel string) error {
	dirs := strings.Split(path.Dir(rel), "/")
	cur := c.cfg.RemoteDir
	if cur == "" {
		cur = "/"
	}
	cur = strings.TrimRight(cur, "/")
	for _, d := range dirs {
		if d == "" || d == "." {
			continue
		}
		if cur == "" {
			cur = "/" + d
		} else {
			cur = cur + "/" + d
		}
		if createdDirs[cur] {
			continue
		}
		if err := c.conn.MakeDir(cur); err != nil {
			// Might already exist than not. Check by listing.
			if _, lerr := c.conn.List(cur); lerr != nil {
				return fmt.Errorf("buat folder %s gagal: %w", cur, err)
			}
		}
		createdDirs[cur] = true
	}
	return nil
}

// progressActive diset saat stderr adalah terminal interaktif (TTY).
// Jika bukan TTY (pipe, CI, log) bar disembunyikan agar output tetap bersih.
var progressActive = term.IsTerminal(int(os.Stderr.Fd()))

// newFileBar builds a per-file progress bar that reports bytes in human
// units (MB) + percent; total<=0 renders an indeterminate spinner.
func newFileBar(desc string, total int64) *progressbar.ProgressBar {
	opts := []progressbar.Option{
		progressbar.OptionSetWriter(os.Stderr),
		progressbar.OptionSetDescription("  " + desc),
		progressbar.OptionSetWidth(40),
		progressbar.OptionShowBytes(true),
		progressbar.OptionThrottle(80 * time.Millisecond),
	}
	if total > 0 {
		return progressbar.NewOptions64(total, opts...)
	}
	return progressbar.NewOptions(-1, opts...)
}

// progressReader advances the bar for every chunk read from the underlying
// stream, so Stor/Retr drive the progress automatically.
type progressReader struct {
	r   io.Reader
	bar *progressbar.ProgressBar
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		_ = p.bar.Add(n)
	}
	return n, err
}

// Upload copies a local file to the remote tree (with progress).
func (c *DeployClient) Upload(localAbs, rel string) error {
	if err := c.EnsureRemoteDir(rel); err != nil {
		return err
	}
	f, err := os.Open(localAbs)
	if err != nil {
		return err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return err
	}

	if !progressActive {
		return c.conn.Stor(c.remoteAbs(rel), f)
	}
	bar := newFileBar("UP "+rel, st.Size())
	defer bar.Finish()
	return c.conn.Stor(c.remoteAbs(rel), &progressReader{r: f, bar: bar})
}

// Download fetches a remote file to a local path (with progress).
func (c *DeployClient) Download(remoteRel, localAbs string) error {
	os.MkdirAll(path.Dir(localAbs), 0o755)
	resp, err := c.conn.Retr(c.remoteAbs(remoteRel))
	if err != nil {
		return err
	}
	defer resp.Close()
	out, err := os.Create(localAbs)
	if err != nil {
		return err
	}
	defer out.Close()

	if !progressActive {
		_, err = io.Copy(out, resp)
		return err
	}
	// FileSize needs its own data connection, so fetch it before Retr's is
	// consumed; -1 means the server did not answer (indeterminate bar).
	size, err := c.conn.FileSize(c.remoteAbs(remoteRel))
	if err != nil {
		size = -1
	}
	bar := newFileBar("DL "+remoteRel, size)
	defer bar.Finish()
	_, err = io.Copy(out, &progressReader{r: resp, bar: bar})
	return err
}

// DeleteRemote removes a remote file.
func (c *DeployClient) DeleteRemote(rel string) error {
	return c.conn.Delete(c.remoteAbs(rel))
}