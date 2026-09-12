package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHashConcurrent(t *testing.T) {
	dir := t.TempDir()
	paths := []string{
		filepath.Join(dir, "a.txt"),
		filepath.Join(dir, "b.bin"),
		filepath.Join(dir, "c.log"),
	}
	contents := []byte("hello world\n")
	if err := os.WriteFile(paths[0], []byte("alpha beta\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths[1], []byte("gamma\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths[2], contents, 0o600); err != nil {
		t.Fatal(err)
	}

	files := make([]*LocalFile, 0, len(paths))
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, &LocalFile{RelPath: filepath.Base(p), AbsPath: p, Size: st.Size(), Mtime: st.ModTime().UnixNano()})
	}

	if err := hashConcurrent(files); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		want, err := HashFile(f.AbsPath)
		if err != nil {
			t.Fatal(err)
		}
		if f.Hash != want {
			t.Fatalf("hashConcurrent(%s) = %s, want %s", f.RelPath, f.Hash, want)
		}
	}
}

func TestLocalIndexCachesBySizeMtime(t *testing.T) {
	dir := t.TempDir()
	rel := "a.txt"
	if err := os.WriteFile(filepath.Join(dir, rel), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{LocalDir: dir, Ignore: []string{}}
	idx1, err := cfg.localIndex(nil)
	if err != nil {
		t.Fatal(err)
	}
	f1 := idx1[rel]
	if f1 == nil || f1.Hash == "" {
		t.Fatal("first index did not hash file")
	}

	// Corrupt the cached hash on purpose: a cache hit must reuse it verbatim.
	state := &State{Version: 1, Files: map[string]StateFile{
		rel: {LocalHash: "deadbeef", LocalSize: f1.Size, LocalMtime: f1.Mtime},
	}}

	idx2, err := cfg.localIndex(state)
	if err != nil {
		t.Fatal(err)
	}
	if idx2[rel].Hash != "deadbeef" {
		t.Fatalf("size+mtime cache miss: got %s, want cached deadbeef", idx2[rel].Hash)
	}

	// Change only the mtime (same size/name): the cache key breaks and the
	// file must be re-hashed from disk.
	st, err := os.Stat(filepath.Join(dir, rel))
	if err != nil {
		t.Fatal(err)
	}
	state.Files[rel] = StateFile{LocalHash: "deadbeef", LocalSize: f1.Size, LocalMtime: st.ModTime().UnixNano() + 1}
	idx3, err := cfg.localIndex(state)
	if err != nil {
		t.Fatal(err)
	}
	if idx3[rel].Hash == "deadbeef" {
		t.Fatal("expected re-hash when mtime changes")
	}
	if idx3[rel].Hash != f1.Hash {
		t.Fatalf("re-hash mismatch: got %s want %s", idx3[rel].Hash, f1.Hash)
	}
}

func TestSortedLocalOrder(t *testing.T) {
	idx := map[string]*LocalFile{
		"z.txt": {RelPath: "z.txt"},
		"a/b":   {RelPath: "a/b"},
		"m.txt": {RelPath: "m.txt"},
	}
	out := sortedLocal(idx)
	for i := 1; i < len(out); i++ {
		if out[i-1].RelPath >= out[i].RelPath {
			t.Fatalf("not sorted: %s >= %s", out[i-1].RelPath, out[i].RelPath)
		}
	}
}

func TestSyncRemoteMetaTouchedDirs(t *testing.T) {
	state := &State{Version: 1, Files: map[string]StateFile{
		"assets/css/app.css": {LocalHash: "h1"},
		"index.html":         {LocalHash: "h2"},
		"untouched.txt":      {LocalHash: "h3"},
	}}
	rels := map[string]bool{
		"assets/css/app.css": true,
		"index.html":         true,
	}
	client := &probeClient{}
	if err := syncRemoteMeta(client, state, rels); err != nil {
		t.Fatal(err)
	}
	sf := state.Files["assets/css/app.css"]
	if sf.RemoteSize == 0 || sf.RemoteTime == "" {
		t.Fatalf("meta not recorded: %+v", sf)
	}
	if u := state.Files["untouched.txt"]; u.RemoteSize != 0 {
		t.Fatal("untouched file must not get meta")
	}
}

type probeClient struct{}

func (p *probeClient) ListRemoteDirs(_ []string) ([]*RemoteFile, error) {
	return []*RemoteFile{
		{RelPath: "assets/css/app.css", Size: 123, Time: time.Now().UTC()},
	}, nil
}