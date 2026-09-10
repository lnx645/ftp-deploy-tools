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

var createdDirs = map[string]bool{}

// EnsureRemoteDir creates remote directories for the parent of relPath.
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

// Upload copies a local file to the remote tree.
func (c *DeployClient) Upload(localAbs, rel string) error {
	if err := c.EnsureRemoteDir(rel); err != nil {
		return err
	}
	f, err := os.Open(localAbs)
	if err != nil {
		return err
	}
	defer f.Close()
	return c.conn.Stor(c.remoteAbs(rel), f)
}

// Download fetches a remote file to a local path.
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
	_, err = io.Copy(out, resp)
	return err
}

// DeleteRemote removes a remote file.
func (c *DeployClient) DeleteRemote(rel string) error {
	return c.conn.Delete(c.remoteAbs(rel))
}