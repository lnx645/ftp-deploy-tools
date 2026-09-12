package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func prompt(label, def string) string {
	reader := bufio.NewReader(os.Stdin)
	if def != "" {
		fmt.Printf("%s [%s]: ", label, def)
	} else {
		fmt.Printf("%s: ", label)
	}
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

func runInitConfig(localDir string) error {
	existing := defaultConfig()
	if cur, err := LoadConfig(localDir); err == nil {
		existing = cur
	}

	fmt.Println("=== Konfigurasi FTP Deploy ===")
	cfg := &Config{
		Port: existing.Port,
		RemoteDir:  prompt("Remote dir", existing.RemoteDir),
		LocalDir:   localDir,
		Ignore:     existing.Ignore,
		PassiveMode: existing.PassiveMode,
	}
	if cfg.Port == 0 {
		cfg.Port = 21
	}

	// Host
	host := prompt("FTP host", fmt.Sprintf("%s", existing.Host))
	cfg.Host = host

	// Port
	portStr := prompt("FTP port", fmt.Sprintf("%d", cfg.Port))
	if p, err := parsePort(portStr); err == nil {
		cfg.Port = p
	}

	// Username
	user := prompt("Username", existing.Username)
	cfg.Username = user

	// Password (silent-ish: accessible via env override too)
	if existing.Password != "" {
		pw := prompt("Password (kosongkan untuk pakai %s)", "******")
		if pw != "" && pw != "******" {
			cfg.Password = pw
		} else {
			cfg.Password = existing.Password
		}
	} else {
		cfg.Password = prompt("Password", "")
	}

	// Passive
	passive := prompt("Passive mode (y/n)", "y")
	cfg.PassiveMode = passive != "n" && passive != "N"

	if err := SaveConfig(cfg); err != nil {
		return fmt.Errorf("simpan config gagal: %w", err)
	}
	fmt.Println("Config tersimpan di .ftpdeploy/config.json")
	fmt.Println("Keamanan: password dienkripsi DPAPI (user+machine Windows) — sama seperti Git Credential Manager.")
	fmt.Println("Tips: override password via env FTPDEPLOY_PASSWORD jika perlu.")
	return nil
}

func parsePort(s string) (int, error) {
	var p int
	if _, err := fmt.Sscanf(s, "%d", &p); err != nil {
		return 0, err
	}
	return p, nil
}