package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const version = "0.1.0"

func usage() {
	fmt.Println(`ftpd — FTP deploy tool (git-style)

Penggunaan:
  ftpd init [--delete]        Full sync: upload SEMUA file local ke server
  ftpd push [--delete] [--dry-run] [--force]   Upload hanya file yang berubah
  ftpd pull [--delete] [--dry-run]   Download file yang berubah di server
  ftpd status                  Lihat diff local vs server (tanpa mengubah)
  ftpd log                     Riwayat deploy
  ftpd config                  Tampilkan konfigurasi saat ini
  ftpd init-config             Buat konfigurasi interaktif
  ftpd help                    Bantuan ini

Flag:
  --delete       Juga hapus file yang tidak ada di sisi local/server
  --dry-run      Hanya simulasi, tidak ubah apa-apa
  --force        Timpa file yang konflik (push hanya)

Konfigurasi: .ftpdeploy/config.json di project directory.`)
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		os.Exit(1)
	}
	cmd := args[0]
	var flags []string
	if len(args) > 1 {
		flags = args[1:]
	}

	has := func(name string) bool {
		for _, f := range flags {
			if f == name {
				return true
			}
		}
		return false
	}

	localDir, _ := os.Getwd()

	switch cmd {
	case "help", "-h", "--help":
		usage()
		return
	case "log":
		n := len(loadHistory(localDir))
		for _, f := range flags {
			if strings.HasPrefix(f, "-n") {
				v := strings.TrimPrefix(f, "-n")
				v = strings.TrimPrefix(v, "=")
				if i, err := strconv.Atoi(v); err == nil {
					n = i
				}
			}
		}
		os.Exit(runLogAndExit(localDir, n))
	case "config":
		cfg, err := LoadConfig(localDir)
		if err != nil {
			fmt.Printf("Config belum ada: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Host:     %s:%d\n", cfg.Host, cfg.Port)
		fmt.Printf("User:     %s\n", cfg.Username)
		fmt.Printf("Remote:   %s\n", cfg.RemoteDir)
		fmt.Printf("Local:    %s\n", cfg.LocalDir)
		fmt.Printf("Ignore:   %d pola\n", len(cfg.Ignore))
		return
	case "init-config":
		if err := runInitConfig(localDir); err != nil {
			fmt.Println("ERROR:", err)
			os.Exit(1)
		}
		return
	}

	cfg, err := LoadConfig(localDir)
	if err != nil {
		fmt.Printf("Config belum ada. Jalankan dulu: ftpd init-config\n%v\n", err)
		os.Exit(1)
	}

	client, err := Connect(cfg)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer client.Close()

	switch cmd {
	case "init":
		err = runInit(cfg, client, has("--delete"))
	case "push":
		err = runPush(cfg, client, has("--delete"), has("--dry-run"), has("--force"))
	case "pull":
		err = runPull(cfg, client, has("--delete"), has("--dry-run"))
	case "status":
		err = runStatus(cfg, client)
	default:
		usage()
		os.Exit(1)
	}
	if err != nil {
		fmt.Println("ERROR:", err)
		os.Exit(1)
	}
}

func runLogAndExit(localDir string, n int) int {
	if err := runLog(&Config{LocalDir: localDir}, n); err != nil {
		fmt.Println("ERROR:", err)
		return 1
	}
	return 0
}