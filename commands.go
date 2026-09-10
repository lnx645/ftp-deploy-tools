package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func runInit(cfg *Config, client *DeployClient, deleteRemote bool) error {
	fmt.Println("=== INIT: full sync local -> server ===")
	locIdx, err := cfg.localIndex()
	if err != nil {
		return err
	}
	state := &State{Version: 1, Files: map[string]StateFile{}}

	var uploaded int
	var deleted int
	var bytes int64
	uploadedRels := map[string]bool{}

	// Upload every local file.
	for _, lf := range sortedLocal(locIdx) {
		if err := client.Upload(lf.AbsPath, lf.RelPath); err != nil {
			return err
		}
		uploaded++
		bytes += lf.Size
		state.Files[lf.RelPath] = StateFile{LocalHash: lf.Hash}
		uploadedRels[lf.RelPath] = true
		fmt.Printf("  [U] %s\n", lf.RelPath)
	}

	if deleteRemote {
		remotes, err := client.ListRemote()
		if err != nil {
			return err
		}
		for _, r := range remotes {
			if _, ok := locIdx[r.RelPath]; !ok {
				if err := client.DeleteRemote(r.RelPath); err != nil {
					return err
				}
				deleted++
				fmt.Printf("  [D] %s\n", r.RelPath)
			}
		}
	}

	if err := syncRemoteMeta(client, state, uploadedRels); err != nil {
		return err
	}

	if err := state.Save(cfg.LocalDir); err != nil {
		return err
	}
	appendHistory(cfg.LocalDir, HistoryEntry{
		Time: remoteNow(), Action: "init",
		Uploaded: uploaded, Deleted: deleted, Bytes: bytes,
	})
	fmt.Printf("\nSelesai. Upload %d file (%.1f MB), delete %d.\n", uploaded, float64(bytes)/1e6, deleted)
	return nil
}

// syncRemoteMeta records remote size/time for freshly uploaded files so the
// next diff does not mistake them for "changed on server".
func syncRemoteMeta(client *DeployClient, state *State, rels map[string]bool) error {
	if len(rels) == 0 {
		return nil
	}
	remotes, err := client.ListRemote()
	if err != nil {
		return err
	}
	for _, r := range remotes {
		if !rels[r.RelPath] {
			continue
		}
		sf, ok := state.Files[r.RelPath]
		if !ok {
			continue
		}
		sf.RemoteSize = r.Size
		sf.RemoteTime = r.Time.UTC().Format("2006-01-02T15:04:05Z")
		state.Files[r.RelPath] = sf
	}
	return nil
}

func sortedLocal(idx map[string]*LocalFile) []*LocalFile {
	keys := make([]string, 0, len(idx))
	for k := range idx {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	out := make([]*LocalFile, 0, len(keys))
	for _, k := range keys {
		out = append(out, idx[k])
	}
	return out
}

func runPush(cfg *Config, client *DeployClient, deleteRemote bool, dryRun bool, force bool) error {
	state, err := LoadState(cfg.LocalDir)
	if err != nil {
		return err
	}
	d, err := cfg.diff(client, state)
	if err != nil {
		return err
	}

	fmt.Println("=== PUSH: local -> server ===")
	printDiff(d, true)

	if dryRun {
		fmt.Println("\nDRY RUN — tidak ada yang diubah.")
		return nil
	}

	// Skip conflicted files unless forced, mirroring git's non-fast-forward rejection.
	skip := map[string]bool{}
	for _, rel := range d.Conflicts {
		if !force {
			skip[rel] = true
		}
	}

	var uploaded int
	var deleted int
	var bytes int64
	uploadedRels := map[string]bool{}

	for _, rel := range d.Added {
		if skip[rel] {
			fmt.Printf("  [~] SKIP (konflik) %s\n", rel)
			continue
		}
		lf := d.Local[rel]
		if err := client.Upload(lf.AbsPath, rel); err != nil {
			return err
		}
		hash, _ := HashFile(lf.AbsPath)
		state.Files[rel] = StateFile{LocalHash: hash}
		uploaded++
		bytes += lf.Size
		uploadedRels[rel] = true
		fmt.Printf("  [U] %s\n", rel)
	}
	for _, rel := range d.Modified {
		if skip[rel] {
			fmt.Printf("  [~] SKIP (konflik) %s\n", rel)
			continue
		}
		lf := d.Local[rel]
		if err := client.Upload(lf.AbsPath, rel); err != nil {
			return err
		}
		hash, _ := HashFile(lf.AbsPath)
		state.Files[rel] = StateFile{LocalHash: hash}
		uploaded++
		bytes += lf.Size
		uploadedRels[rel] = true
		fmt.Printf("  [U~] %s\n", rel)
	}

	if deleteRemote {
		for _, rel := range d.Deleted {
			if skip[rel] {
				continue
			}
			if err := client.DeleteRemote(rel); err != nil {
				return err
			}
			delete(state.Files, rel)
			deleted++
			fmt.Printf("  [D] %s\n", rel)
		}
	}

	if err := syncRemoteMeta(client, state, uploadedRels); err != nil {
		return err
	}

	if err := state.Save(cfg.LocalDir); err != nil {
		return err
	}
	if len(skip) > 0 {
		fmt.Println("Konflik di-skip: jalankan `ftpd pull` dulu untuk merge, atau `ftpd push --force` untuk menimpa.")
	}
	appendHistory(cfg.LocalDir, HistoryEntry{
		Time: remoteNow(), Action: "push",
		Uploaded: uploaded, Deleted: deleted, Bytes: bytes,
	})
	fmt.Printf("\nPush selesai: upload %d file (%.1f MB), delete %d.\n", uploaded, float64(bytes)/1e6, deleted)
	return nil
}

func runPull(cfg *Config, client *DeployClient, deleteLocal bool, dryRun bool) error {
	state, err := LoadState(cfg.LocalDir)
	if err != nil {
		return err
	}
	d, err := cfg.diff(client, state)
	if err != nil {
		return err
	}

	fmt.Println("=== PULL: server -> local ===")
	printDiff(d, true)

	if dryRun {
		fmt.Println("\nDRY RUN — tidak ada yang diubah.")
		return nil
	}

	// If a file changed locally too, pulling would silently destroy work -> skip.
	locMutated := append([]string{}, d.Modified...)
	locMutated = append(locMutated, d.Added...)
	locSet := map[string]bool{}
	for _, rel := range locMutated {
		locSet[rel] = true
	}

	var downloaded int
	var deleted int
	var bytes int64

	downloads := append([]string{}, d.RemoteChanged...)
	downloads = append(downloads, d.RemoteNew...)
	for _, rel := range downloads {
		if locSet[rel] {
			fmt.Printf("  [~] SKIP (berubah lokal, Konflik) %s\n", rel)
			continue
		}
		localAbs := filepath.Join(cfg.LocalDir, filepath.FromSlash(rel))
		if err := client.Download(rel, localAbs); err != nil {
			return err
		}
		hash, _ := HashFile(localAbs)
		rf := d.Remote[rel]
		state.Files[rel] = StateFile{
			LocalHash:  hash,
			RemoteSize: rf.Size,
			RemoteTime: rf.Time.UTC().Format("2006-01-02T15:04:05Z"),
		}
		downloaded++
		bytes += rf.Size
		fmt.Printf("  [D] %s\n", rel)
	}

	// Files that exist locally but no longer on server.
	if deleteLocal {
		locIdx, _ := cfg.localIndex()
		for _, lf := range sortedLocal(locIdx) {
			if _, ok := d.Remote[lf.RelPath]; !ok {
				if _, inState := state.Files[lf.RelPath]; inState {
					if err := os.Remove(lf.AbsPath); err != nil {
						return err
					}
					delete(state.Files, lf.RelPath)
					deleted++
					fmt.Printf("  [L-D] %s\n", lf.RelPath)
				}
			}
		}
	}

	if err := state.Save(cfg.LocalDir); err != nil {
		return err
	}
	appendHistory(cfg.LocalDir, HistoryEntry{
		Time: remoteNow(), Action: "pull",
		Downloaded: downloaded, Deleted: deleted, Bytes: bytes,
	})
	fmt.Printf("\nPull selesai: download %d file (%.1f MB), delete local %d.\n", downloaded, float64(bytes)/1e6, deleted)
	return nil
}

func runStatus(cfg *Config, client *DeployClient) error {
	state, err := LoadState(cfg.LocalDir)
	if err != nil {
		return err
	}
	d, err := cfg.diff(client, state)
	if err != nil {
		return err
	}
	fmt.Println("=== STATUS ===")
	printDiff(d, true)
	fmt.Printf("\n%s\n", d.Summarize())
	return nil
}

func runLog(cfg *Config, n int) error {
	h := loadHistory(cfg.LocalDir)
	if len(h) == 0 {
		fmt.Println("Belum ada riwayat deploy.")
		return nil
	}
	if n <= 0 || n > len(h) {
		n = len(h)
	}
	start := len(h) - n
	for i := start; i < len(h); i++ {
		e := h[i]
		parts := []string{
			strings.TrimPrefix(e.Time, ""),
			e.Action,
		}
		if e.Uploaded > 0 {
			parts = append(parts, fmt.Sprintf("up:%d", e.Uploaded))
		}
		if e.Downloaded > 0 {
			parts = append(parts, fmt.Sprintf("down:%d", e.Downloaded))
		}
		if e.Deleted > 0 {
			parts = append(parts, fmt.Sprintf("del:%d", e.Deleted))
		}
		parts = append(parts, fmt.Sprintf("%.1fMB", float64(e.Bytes)/1e6))
		fmt.Println(strings.Join(parts, "  "))
	}
	return nil
}