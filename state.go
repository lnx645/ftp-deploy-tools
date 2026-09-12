package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type RemoteMeta struct {
	Size int64  `json:"size"`
	Time string `json:"time"`
}

type StateFile struct {
	LocalHash   string `json:"localHash"`
	LocalSize   int64  `json:"localSize,omitempty"`
	LocalMtime  int64  `json:"localMtime,omitempty"`
	RemoteSize  int64  `json:"remoteSize,omitempty"`
	RemoteTime  string `json:"remoteTime,omitempty"`
}

type State struct {
	Version int                  `json:"version"`
	Files   map[string]StateFile `json:"files"`
}

func statePath(localDir string) string {
	return filepath.Join(localDir, ".ftpdeploy", "state.json")
}

func historyPath(localDir string) string {
	return filepath.Join(localDir, ".ftpdeploy", "history.json")
}

func LoadState(localDir string) (*State, error) {
	data, err := os.ReadFile(statePath(localDir))
	if err != nil {
		if os.IsNotExist(err) {
			return &State{Version: 1, Files: map[string]StateFile{}}, nil
		}
		return nil, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("format state.json salah: %w", err)
	}
	if s.Files == nil {
		s.Files = map[string]StateFile{}
	}
	if s.Version == 0 {
		s.Version = 1
	}
	return &s, nil
}

func (s *State) Save(localDir string) error {
	dir := filepath.Join(localDir, ".ftpdeploy")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statePath(localDir), data, 0o600)
}

// hashBufSize is the read buffer used per hashing worker (shared between files).
const hashBufSize = 1 << 20

func HashFile(path string) (string, error) {
	return hashFileBuffer(path, make([]byte, hashBufSize))
}

// hashFileBuffer hashes a file with a caller-owned buffer, so concurrent
// workers reuse one allocation instead of allocating 1 MiB per file.
func hashFileBuffer(path string, buf []byte) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.CopyBuffer(h, f, buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}