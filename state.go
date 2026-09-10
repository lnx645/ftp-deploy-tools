package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
)

type RemoteMeta struct {
	Size int64  `json:"size"`
	Time string `json:"time"`
}

type StateFile struct {
	LocalHash  string     `json:"localHash"`
	RemoteSize int64      `json:"remoteSize,omitempty"`
	RemoteTime string     `json:"remoteTime,omitempty"`
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

func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := copyBuffer(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// keyForState is a stable key used to derive a short fingerprint.
func keyForState(sf StateFile) uint32 {
	h := fnv.New32a()
	h.Write([]byte(sf.LocalHash))
	h.Write([]byte(fmt.Sprintf("|%d", sf.RemoteSize)))
	h.Write([]byte(sf.RemoteTime))
	return h.Sum32()
}