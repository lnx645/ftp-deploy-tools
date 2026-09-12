# ftpd — FTP Deploy Tool

**Deploy aplikasi ke server FTP dengan alur kerja ala Git.** Tidak perlu lagi meng-upload file satu per satu lewat FileZilla atau cPanel. `ftpd` melacak status file lokal vs server, tahu file mana yang berubah, dan hanya meng-upload/download yang perlu — lengkap dengan deteksi konflik.

---

## Deskripsi Tools

`ftpd` adalah CLI (command-line tool) yang ditulis dalam **Go** untuk mensinkronkan direktori lokal dengan direktori remote via **FTP**. Konsepnya meniru `git push` / `git pull` / `git status`, tetapi yang disinkronkan bukan repository Git, melainkan file website/proyek biasa ke server hosting.

Cara kerjanya:

1. **Indeks lokal** — seluruh file di folder proyek di-hash (SHA-256), mengabaikan pola tertentu (ignore), seperti `.gitignore`.
2. **Indeks remote** — `ftpd` berjalan (walk) melintasi seluruh direktori FTP untuk mengetahui file, ukuran, dan timestamp di sisi server.
3. **State** — hasil sinkronisasi terakhir disimpan di `.ftpdeploy/state.json`. Perubahan diukur relatif terhadap state ini.
4. **Diff** — `ftpd` membandingkan ketiganya (lokal, remote, state) untuk menentukan file yang baru, dimodifikasi, dihapus, atau **konflik** (berubah di dua sisi).
5. **Eksekusi** — hanya file yang benar-benar berubah yang ditransfer, dengan progress bar per file.

> **Konflik** terjadi ketika sebuah file berubah di sisi lokal **dan** berubah di sisi server sejak sinkronisasi terakhir (data divergen, seperti non-fast-forward di Git). `ftpd` menolak menimpa dan menyuruh Anda memutuskan: `git pull` untuk mengambil perubahan server, atau `--force` untuk menimpa.

---

## Fitur Utama

- **Git-style workflow** — `init`, `push`, `pull`, `status`, `log`, `config`.
- **Sinkronisasi inkremental** — hanya transfer file yang berubah (hemat bandwidth & waktu).
- **Deteksi konflik** — file yang berubah di dua sisi tidak akan ditimpa tanpa persetujuan.
- **Dry run** — lihat apa yang akan terjadi tanpa mengubah apa pun (`--dry-run`).
- **Hapus otomatis (opsional)** — `--delete` membersihkan file yang tidak lagi ada di sisi lain.
- **Progress bar** — tampil saat terminal interaktif, otomatis disembunyikan di CI/pipe/log.
- **Password terenkripsi (Windows DPAPI)** — password disimpan terenkripsi, terikat ke user + mesin, seperti Git Credential Manager.
- **Override password via environment** — `FTPDEPLOY_PASSWORD` untuk CI/CD, tidak perlu password di config.
- **Ignore patterns** — pola ala `.gitignore` termasuk `**`, dengan pola reserved yang selalu dilindungi (`.git/**`, `.ftpdeploy/**`, `.env*`).
- **Mode pasif FTP** — default aktif, bisa dimatikan untuk firewall lama.
- **Penanganan Windows yang aman** — symlink/junction (reparse point) dilewati agar tidak terjadi loop atau error baca.

---

## Persyaratan

- Go 1.26+ (untuk build dari source) — atau jalankan langsung binary `ftpd.exe` (Windows).
- Akses FTP dengan kredensial (host, port, username, password), misalnya dari hosting cPanel.

---

## Instalasi / Build

Unduh `ftpd.exe` (Windows) atau build dari source:

```bash
go build -o ftpd.exe .
```

Tambahkan folder binary ke `PATH`, atau jalankan lewat `./ftpd` / `ftpd.exe`.

---

## Quick Start

Jalankan dari **dalam folder proyek** yang ingin di-deploy:

```bash
# 1. Buat konfigurasi interaktif
ftpd init-config

# 2. Upload SEMUA file ke server (sinkronisasi penuh pertama)
ftpd init

# 3. Setelah mengubah file, push hanya yang berubah
ftpd push

# 4. Lihat perbedaan lokal vs server kapan saja
ftpd status
```

---

## Perintah (Commands)

| Perintah | Deskripsi |
|---|---|
| `ftpd init [--delete]` | Sinkronisasi penuh: upload **semua** file lokal ke server, tulis state pertama. |
| `ftpd push [--delete] [--dry-run] [--force]` | Upload hanya file yang berubah dari lokal ke server. |
| `ftpd pull [--delete] [--dry-run]` | Download hanya file yang berubah dari server ke lokal. |
| `ftpd status` | Tampilkan diff lokal vs server tanpa mengubah apa pun. |
| `ftpd log [-n<span>N</span>]` | Riwayat deploy (jumlah file, jumlah MB). `-n20` untuk 20 entri terakhir. |
| `ftpd config` | Tampilkan konfigurasi saat ini (termasuk status enkripsi password). |
| `ftpd init-config` | Buat/edit konfigurasi secara interaktif. |
| `ftpd help` | Bantuan ini. |

### Flag (Options)

| Flag | Berlaku di | Deskripsi |
|---|---|---|
| `--delete` | `init`, `push`, `pull` | Juga hapus file yang tidak ada di sisi lain (server untuk push, lokal untuk pull). |
| `--dry-run` | `push`, `pull` | Hanya simulasi — tampilkan yang akan dilakukan, tanpa mengubah apa pun. |
| `--force` | `push` | Timpa file yang berkonflik (abaikan penolakan konflik). |

---

## Alur Kerja Harian

### 1. Inisialisasi

```bash
ftpd init-config    # isi host, port, user, password, remote dir
ftpd init           # upload semua file, buat state
```

### 2. Update / Rilis

```bash
# -- edit file di lokal --
ftpd status         # cek file apa saja yang berubah & apakah ada konflik
ftpd push           # kirim perubahan
```

### 3. Menarik perubahan dari server (mis. ada yang edit via cPanel)

```bash
ftpd status         # lihat file yang berubah di server
ftpd pull           # download perubahan server ke lokal
```

### 4. Menangani konflik

Bila ada file yang berubah di **kedua** sisi, `push` akan meng-*skip* file itu:

```bash
ftpd push           # konflik di-skip
ftpd pull           # ambil versi server, lalu satukan manual
```

…atau tegas ingin menimpa versi lokal:

```bash
ftpd push --force   # suka-suka Anda, versi lokal menang
```

### 5. Menghapus file yang dihapus

```bash
ftpd push --delete    # hapus file di server yang sudah tidak ada di lokal
ftpd pull --delete    # hapus file di lokal yang sudah tidak ada di server
```

### 6. Uji coba tanpa risiko

```bash
ftpd push --dry-run   # lihat dulu, tidak ada yang berubah
```

---

## Konfigurasi

Config disimpan di `.ftpdeploy/config.json` di dalam folder proyek. Isi via `ftpd init-config` atau tulis manual:

```json
{
  "host": "ftp.example.com",
  "port": 21,
  "username": "user@example.com",
  "password": "enc:...base64...",
  "remoteDir": "/public_html",
  "localDir": ".",
  "ignore": [".git/**", ".ftpdeploy/**", "node_modules/**"],
  "passiveMode": true
}
```

| Field | Deskripsi |
|---|---|
| `host` | Hostname/server FTP. |
| `port` | Port FTP (default `21`). |
| `username` | Username login FTP. |
| `password` | Password. Ditulis terenkripsi DPAPI (`enc:...`) saat disimpan via `ftpd init-config`. |
| `remoteDir` | Direktori tujuan di server, mis. `/public_html`. |
| `localDir` | Direktori proyek lokal (`"."` = folder saat ini). |
| `ignore` | Daftar pola ignore (lihat di bawah). |
| `passiveMode` | Gunakan FTP passive mode (default `true`). |

> Catatan: `.ftpdeploy/` otomatis selalu di-ignore dan berisi file state/riwayat — jangan di-upload dan jangan di-commit.

### Pola Ignore

Dukungan pola ala `.gitignore`:

| Pola | Sama dengan |
|---|---|
| `.git/**` | Seluruh isi folder `.git`. |
| `node_modules/**` | Seluruh isi `node_modules` (di level mana pun). |
| `node_modules` | Semua folder bernama `node_modules` (di mana pun). |
| `.env*` | `.env`, `.env.local`, dst. |
| `storage/logs/*.log` | File tertentu. |

Pattern berikut **selalu dilindungi** dan tidak bisa dihilangkan:

- `.git/**`
- `.ftpdeploy/**`
- `.env` dan `.env.*`

---

## Keamanan

- **Password dienkripsi dengan Windows DPAPI** (CryptProtectData) — terikat ke user + mesin, tidak bisa dibuka di komputer lain. Mekanisme yang sama dengan Git Credential Manager.
- **Override password** di lingkungan CI/otomatis tanpa menyentuh config:

  ```bash
  export FTPDEPLOY_PASSWORD="rahasia"
  ftpd push
  ```

  Password dari env ini menang atas yang ada di `config.json`.
- File `state.json` & `history.json` juga ditulis dengan permission `0600`.
- Pada sistem non-Windows di mana DPAPI tidak tersedia, file config tetap ditulis dengan permission `0600`, dan akan muncul peringatan bahwa password disimpan dalam bentuk plaintext.

---

## Riwayat Deploy

Semua aktivitas deploy dicatat di `.ftpdeploy/history.json` (maks 500 entri) dan bisa dibaca lewat:

```bash
ftpd log
ftpd log -n10    # 10 entri terakhir
```

Contoh output:

```
2026-09-12T08:00:00Z  push  up:12  del:1  3.4MB
2026-09-12T09:15:00Z  pull  down:2          0.1MB
2026-09-11T22:00:00Z  init  up:148        128.0MB
```

---

## Tata Letak File

| Lokasi | Isi |
|---|---|
| `ftpd.exe` / `ftpd` | Binary utama. |
| `.ftpdeploy/config.json` | Konfigurasi FTP. |
| `.ftpdeploy/state.json` | State sinkronisasi (hash lokal + meta file remote). |
| `.ftpdeploy/history.json` | Riwayat deploy. |

---

## Contoh Skenario

**Men-deploy project Laravel ke cPanel:**

```bash
cd my-laravel-app
ftpd init-config
# host: ftp.mydomain.com, remoteDir: /public_html

ftpd init
ftpd push
```

File seperti `storage/`, `vendor/`, `bootstrap/cache/`, `.env` otomatis ter-ignore oleh pola default — aman untuk produksi.

---

## Stacks & Lisensi

Dibangun dengan **Go**, menggunakan:

- [`github.com/jlaffaye/ftp`](https://github.com/jlaffaye/ftp) — klien FTP.
- [`github.com/schollz/progressbar/v3`](https://github.com/schollz/progressbar/v3) — progress bar.
- `golang.org/x/term` — deteksi TTY.

Lisensi: **MIT**.