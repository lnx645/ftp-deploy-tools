package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// encPrefix marks a password field that was encrypted with DPAPI.
const encPrefix = "enc:"

func encryptSecret(plain string) (string, error) {
	blob, err := ProtectSecret([]byte(plain))
	if err != nil {
		return "", err
	}
	return encPrefix + base64.StdEncoding.EncodeToString(blob), nil
}

func decryptSecret(stored string) (string, error) {
	if !strings.HasPrefix(stored, encPrefix) {
		return stored, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, encPrefix))
	if err != nil {
		return "", err
	}
	blob, err := UnprotectSecret(raw)
	if err != nil {
		return "", err
	}
	return string(blob), nil
}

type Config struct {
	Host        string   `json:"host"`
	Port        int      `json:"port"`
	Username    string   `json:"username"`
	Password    string   `json:"password"`
	RemoteDir   string   `json:"remoteDir"`
	LocalDir    string   `json:"localDir"`
	Ignore      []string `json:"ignore"`
	PassiveMode bool     `json:"passiveMode"`
}

func defaultIgnore() []string {
	return []string{
		".git/**",
		".ftpdeploy/**",
		".claude/**",
		".agents/**",
		".opencode/**",
		"node_modules/**",
		".env",
		".env.*",
		".vscode/**",
		"storage/**",
		"vendor/**",
		"bootstrap/cache/**",
	}
}

func defaultConfig() *Config {
	return &Config{
		Port:        21,
		RemoteDir:   "/",
		LocalDir:    ".",
		Ignore:      defaultIgnore(),
		PassiveMode: true,
	}
}

func configPath(localDir string) string {
	return filepath.Join(localDir, ".ftpdeploy", "config.json")
}

func LoadConfig(localDir string) (*Config, error) {
	data, err := os.ReadFile(configPath(localDir))
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("format config salah: %w", err)
	}
	if cfg.Port == 0 {
		cfg.Port = 21
	}
	if cfg.RemoteDir == "" {
		cfg.RemoteDir = "/"
	}
	// Decrypt an encrypted password (enc:...) back to plain for use in memory.
	if cfg.Password != "" {
		pw, derr := decryptSecret(cfg.Password)
		if derr != nil {
			return nil, fmt.Errorf("password terenkripsi tidak bisa dibuka (bukan user/machine yang sama?): %w", derr)
		}
		cfg.Password = pw
	}
	// Always protect these paths regardless of user config.
	reserved := []string{".git/**", ".ftpdeploy/**", ".env", ".env.*"}
	for _, r := range reserved {
		if !containsString(cfg.Ignore, r) {
			cfg.Ignore = append(cfg.Ignore, r)
		}
	}
	if len(cfg.Ignore) == 0 {
		cfg.Ignore = defaultIgnore()
	}
	return &cfg, nil
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func SaveConfig(cfg *Config) error {
	dir := filepath.Join(cfg.LocalDir, ".ftpdeploy")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	copy := *cfg
	// Encrypt the password before writing it to disk.
	if copy.Password != "" && !strings.HasPrefix(copy.Password, encPrefix) {
		enc, err := encryptSecret(copy.Password)
		if err != nil {
			// DPAPI unavailable: persist plain (non-Windows fallback) but note it.
			fmt.Printf("Peringatan: password tidak terenkripsi (%v)\n", err)
		} else {
			copy.Password = enc
		}
	}
	data, err := json.MarshalIndent(copy, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(cfg.LocalDir), data, 0o600)
}

// matchSegment checks whether a single path segment matches a glob pattern.
func matchSegment(seg, pattern string) bool {
	ok, err := filepath.Match(pattern, seg)
	return err == nil && ok
}

// matchPattern resolves "**" patterns by falling back to a simple recursive check.
func matchPattern(relPath, pattern string) bool {
	cleaned := filepath.ToSlash(relPath)
	pat := filepath.ToSlash(pattern)

	if pat == "**" || pat == "/" {
		return true
	}

	ok, err := filepath.Match(pat, cleaned)
	if err == nil && ok {
		return true
	}

	if strings.HasSuffix(pat, "/**") {
		base := strings.TrimSuffix(pat, "/**")
		if strings.HasPrefix(cleaned, strings.TrimSuffix(base, "/")) {
			return true
		}
	}

	if strings.HasSuffix(pat, ".*") && strings.HasPrefix(filepath.Base(cleaned), strings.TrimSuffix(filepath.Base(pat), ".*")) {
		return true
	}

	return false
}

// IsIgnored reports whether a relative path (or any of its ancestors) is ignored.
func (c *Config) IsIgnored(relPath string) bool {
	segments := strings.Split(filepath.ToSlash(relPath), "/")
	for _, pattern := range c.Ignore {
		if matchPattern(relPath, pattern) {
			return true
		}
		// Match any directory segment, e.g. "node_modules" marks the whole subtree.
		segOnly := strings.TrimSuffix(strings.TrimPrefix(filepath.ToSlash(pattern), "/"), "/")
		if !strings.Contains(segOnly, "/") && !strings.Contains(segOnly, "*") {
			for _, seg := range segments {
				if seg == segOnly {
					return true
				}
			}
		}
	}
	return false
}

type LocalFile struct {
	RelPath string
	AbsPath string
	Size    int64
	Mtime   int64
	Hash    string
}

// WalkLocal lists all local files (respecting ignore), computes SHA-256 hashes.
func (c *Config) WalkLocal() ([]*LocalFile, error) {
	var files []*LocalFile
	root, err := filepath.Abs(c.LocalDir)
	if err != nil {
		return nil, err
	}
	if err := c.walkLocal(root, "", &files); err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].RelPath < files[j].RelPath })
	return files, nil
}

func (c *Config) walkLocal(dir, rel string, files *[]*LocalFile) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Unreadable dir (broken junction / permission) — skip instead of failing.
		return nil
	}
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		childRel := e.Name()
		if rel != "" {
			childRel = rel + "/" + e.Name()
		}
		if e.IsDir() {
			// Skip reparse points (junctions/symlinks) entirely to avoid loops & read errors.
			if isReparsePoint(full) {
				continue
			}
			if c.IsIgnored(childRel) {
				continue
			}
			if err := c.walkLocal(full, childRel, files); err != nil {
				return err
			}
			continue
		}
		if !e.Type().IsRegular() {
			continue
		}
		if c.IsIgnored(childRel) {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		*files = append(*files, &LocalFile{
			RelPath: filepath.ToSlash(childRel),
			AbsPath: full,
			Size:    info.Size(),
			Mtime:   info.ModTime().UnixNano(),
		})
	}
	return nil
}