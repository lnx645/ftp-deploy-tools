package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"time"
)

type HistoryEntry struct {
	Time       string `json:"time"`
	Action     string `json:"action"`
	Uploaded   int    `json:"uploaded"`
	Downloaded int    `json:"downloaded"`
	Deleted    int    `json:"deleted"`
	Bytes      int64  `json:"bytes"`
}

func loadHistory(localDir string) []HistoryEntry {
	data, err := os.ReadFile(historyPath(localDir))
	if err != nil {
		return nil
	}
	var h []HistoryEntry
	if err := jsonUnmarshal(data, &h); err != nil {
		return nil
	}
	return h
}

func appendHistory(localDir string, e HistoryEntry) error {
	h := loadHistory(localDir)
	if len(h) > 500 {
		h = h[len(h)-500:]
	}
	h = append(h, e)
	data, err := jsonMarshalIndent(h)
	if err != nil {
		return err
	}
	dir := filepath.Join(localDir, ".ftpdeploy")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(historyPath(localDir), data, 0o600)
}

func remoteNow() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// localIndex lists all local files and computes SHA-256 hashes. When state is
// non-nil, files whose size and mtime are unchanged since the last sync reuse
// their cached hash instead of re-reading the whole content — the same
// size+mtime fast path rsync uses. Only changed/new files are hashed, in
// parallel.
func (c *Config) localIndex(state *State) (map[string]*LocalFile, error) {
	files, err := c.WalkLocal()
	if err != nil {
		return nil, err
	}
	idx := make(map[string]*LocalFile, len(files))
	toHash := make([]*LocalFile, 0, len(files))
	for _, f := range files {
		idx[f.RelPath] = f
		if state != nil {
			if sf, ok := state.Files[f.RelPath]; ok && sf.LocalHash != "" &&
				sf.LocalSize == f.Size && sf.LocalMtime == f.Mtime {
				f.Hash = sf.LocalHash
				continue
			}
		}
		toHash = append(toHash, f)
	}
	if err := hashConcurrent(toHash); err != nil {
		return nil, err
	}
	return idx, nil
}

// hashConcurrent hashes files with a bounded worker pool (max 8). Each worker
// reuses one read buffer so memory stays flat regardless of file count.
func hashConcurrent(files []*LocalFile) error {
	if len(files) == 0 {
		return nil
	}
	workers := runtime.NumCPU()
	if workers > 8 {
		workers = 8
	}
	if workers < 2 {
		workers = 2
	}
	jobs := make(chan *LocalFile, workers)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var fatal bool
	worker := func() {
		defer wg.Done()
		buf := make([]byte, hashBufSize)
		for f := range jobs {
			h, err := hashFileBuffer(f.AbsPath, buf)
			if err != nil {
				mu.Lock()
				fatal = true
				mu.Unlock()
				return
			}
			f.Hash = h
		}
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go worker()
	}
	for _, f := range files {
		jobs <- f
	}
	close(jobs)
	wg.Wait()
	if fatal {
		return fmt.Errorf("gagal menghitung hash file")
	}
	return nil
}

// status builds the full diff summary.
func (c *Config) diff(client *DeployClient, state *State) (*Diff, error) {
	locIdx, err := c.localIndex(state)
	if err != nil {
		return nil, err
	}
	remotes, err := client.ListRemote()
	if err != nil {
		return nil, err
	}
	remIdx := map[string]*RemoteFile{}
	for _, r := range remotes {
		remIdx[r.RelPath] = r
	}

	d := &Diff{Local: locIdx, Remote: remIdx, State: state, Added: []string{}, Modified: []string{}, Deleted: []string{}, RemoteChanged: []string{}, RemoteNew: []string{}, Conflicts: []string{}}

	for rel, lf := range locIdx {
		sf, inState := state.Files[rel]
		if !inState {
			d.Added = append(d.Added, rel)
			continue
		}
		if sf.LocalHash != lf.Hash {
			d.Modified = append(d.Modified, rel)
		}
	}
	for rel := range state.Files {
		if _, ok := locIdx[rel]; !ok {
			d.Deleted = append(d.Deleted, rel)
		}
	}
	for rel, rf := range remIdx {
		sf, inState := state.Files[rel]
		if !inState {
			d.RemoteNew = append(d.RemoteNew, rel)
			continue
		}
		mtime := rf.Time.UTC().Format(time.RFC3339)
		if sf.RemoteSize != rf.Size || sf.RemoteTime != mtime {
			d.RemoteChanged = append(d.RemoteChanged, rel)
		}
	}
	computeConflicts(d)
	sortStrings(d.Added)
	sortStrings(d.Modified)
	sortStrings(d.Deleted)
	sortStrings(d.RemoteChanged)
	sortStrings(d.RemoteNew)
	sortStrings(d.Conflicts)
	return d, nil
}

// computeConflicts marks files that changed on both local and remote since the
// last sync (same as git's "diverged"). A file is in conflict when:
//   - it was modified locally AND modified/changed remotely, or
//   - it was added locally AND also appeared remotely (both sides created it).
func computeConflicts(d *Diff) {
	seen := map[string]bool{}
	remotesMutated := map[string]bool{}
	for _, rel := range append(append([]string{}, d.RemoteChanged...), d.RemoteNew...) {
		remotesMutated[rel] = true
	}
	add := func(rel string) {
		if seen[rel] {
			return
		}
		seen[rel] = true
		d.Conflicts = append(d.Conflicts, rel)
	}
	for _, rel := range d.Added {
		if remotesMutated[rel] {
			add(rel)
		}
	}
	for _, rel := range d.Modified {
		if remotesMutated[rel] {
			add(rel)
		}
	}
}

func sortStrings(s []string) {
	slices.Sort(s)
}

type Diff struct {
	Local         map[string]*LocalFile
	Remote        map[string]*RemoteFile
	State         *State
	Added         []string
	Modified      []string
	Deleted       []string
	RemoteChanged []string
	RemoteNew     []string
	Conflicts     []string
}

func (d *Diff) Summarize() string {
	return fmt.Sprintf("to upload: %d new + %d modified, to delete: %d | remote changed: %d, remote new: %d | conflict: %d",
		len(d.Added), len(d.Modified), len(d.Deleted), len(d.RemoteChanged), len(d.RemoteNew), len(d.Conflicts))
}

func printDiff(d *Diff, verbose bool) {
	fmt.Println("=== KONFLIK (ubah di lokal & server) ===")
	for _, rel := range d.Conflicts {
		fmt.Printf("  [!] CONF  %s\n", rel)
	}
	if len(d.Conflicts) == 0 {
		fmt.Println("  (tidak ada)")
	}

	fmt.Println("=== Akan di-upload (local -> server) ===")
	for _, rel := range d.Added {
		fmt.Printf("  [+] NEW  %s\n", rel)
	}
	for _, rel := range d.Modified {
		fmt.Printf("  [~] MOD  %s\n", rel)
	}
	if len(d.Added)+len(d.Modified) == 0 {
		fmt.Println("  (tidak ada)")
	}

	fmt.Println("=== Akan di-delete di server ===")
	for _, rel := range d.Deleted {
		fmt.Printf("  [-] DEL  %s\n", rel)
	}
	if len(d.Deleted) == 0 {
		fmt.Println("  (tidak ada)")
	}

	fmt.Println("=== Berubah di server (untuk pull) ===")
	for _, rel := range d.RemoteChanged {
		fmt.Printf("  [~] MOD  %s (server)\n", rel)
	}
	for _, rel := range d.RemoteNew {
		fmt.Printf("  [+] NEW  %s (server)\n", rel)
	}
	if len(d.RemoteChanged)+len(d.RemoteNew) == 0 {
		fmt.Println("  (tidak ada)")
	}
}