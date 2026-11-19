# Termux Monitor

Panel kontrol Android yang sangat ringan dan sepenuhnya offline, dijalankan sebagai modul Magisk / KernelSU / APatch. Backend ditulis dengan Go modular, frontend memakai Svelte 5 single-page application, dan semuanya terpaket dalam satu binary mandiri tanpa ketergantungan Python, Node.js, atau Bun di perangkat.

- Versi: v1.3.0
- Module id: `termux_monitor`
- Alamat default: `http://<ip-hp>:80` dan `https://<ip-hp>:443`
- Lisensi: GPL-3.0

---

## Daftar Isi

1. [Keunggulan](#keunggulan)
2. [Tab dan Fitur](#tab-dan-fitur)
3. [Instalasi](#instalasi)
4. [Login Pertama](#login-pertama)
5. [Konfigurasi](#konfigurasi)
6. [Kebutuhan Build](#kebutuhan-build)
7. [Build dari Sumber](#build-dari-sumber)
8. [Flavor Build](#flavor-build)
9. [Checklist Verifikasi](#checklist-verifikasi)
10. [Struktur Proyek](#struktur-proyek)
11. [HTTP API](#http-api)
12. [Catatan Keamanan](#catatan-keamanan)
13. [Penanganan Masalah](#penanganan-masalah)
14. [Kredit dan Lisensi](#kredit-dan-lisensi)

---

## Keunggulan

| Aspek | Keterangan |
| :--- | :--- |
| Distribusi | Satu binary Android ARM64 (`webui`) yang dipaket menjadi zip modul Magisk |
| Backend | Go 1.26, paket modular di dalam `internal/`, tanpa CGO (`CGO_ENABLED=0`) |
| Frontend | Svelte 5 + TypeScript + Tailwind CSS 3, dikompilasi Vite 8 lalu ditanam dengan `embed.FS` |
| Offline | Tanpa CDN, tanpa font eksternal, tanpa telemetri, tanpa analytics |
| Transport | HTTP di port 80 dan TLS self-signed di port 443 (dibuat otomatis saat start) |
| Autentikasi | Sesi berbasis cookie (`tmon_session`) dengan password yang dapat diubah |
| WebSocket | Terminal PTY interaktif dan mirroring layar scrcpy |

Seluruh produk berjalan sebagai satu proses. Semua tab berkomunikasi dengan daemon lokal melalui server HTTP yang sama, dan operasi yang membutuhkan hak akses dijalankan lewat `su -c` sehingga WebUI sendiri dapat berjalan sebagai root dari direktori modul.

---

## Tab dan Fitur

### Ringkasan dan Metrik
- `overview` - dashboard berisi beban CPU, memori, penyimpanan, baterai, throughput jaringan dan tombol cepat.
- `sysinfo` - penghitung `/proc` secara detail, frekuensi per core dan informasi governor.
- `processes` - daftar proses berjalan dari `/proc`, dengan deteksi runtime, pemilik, path executable, dan aksi hentikan aman.
- `kernel` - penyetelan cluster CPU, profil governor, parameter VM dan konfigurasi kernel yang tersimpan.
- `network` - penyetelan TCP berbasis sysctl, pemilihan DNS, dan pengaturan TTL yang dibaca dari `network_config.json`.
- `vnstat` - pembungkus pencatatan bandwidth dengan tampilan harian, bulanan dan aksi reset.
- `speedtest` - mulai, pantau, hentikan, dan lihat riwayat pengujian kecepatan.

### Kontrol Android
- `charger` - pengatur batas arus dan tegangan pengisian spesifik perangkat.
- `hotspot` - kontrol SoftAP, daftar klien terhubung melalui pembacaan ARP/lease, filter MAC, dan sinkronisasi pengaturan router.
- `power` - reboot, shutdown, dan aksi daya terjadwal.
- `modem` - hanya telemetri non-proprietary: kualitas sinyal, status SIM, pergantian slot SIM, suhu, dan pemindaian cell.
- `sms` - pembaca inbox dari penyimpanan modem dan basis data SMS ROM.
- `modules` - daftar, toggle, dan instalasi modul Magisk/KernelSU.

### Layanan Sistem
- `ssh` - konfigurasi dan lifecycle daemon Dropbear SSH.
- `telegram` - kontrol jarak jauh berbasis bot dan endpoint proxy jarak jauh yang dilayani WebUI yang sama.
- `tunnel` - ekspos panel lokal melalui binary tunnel, termasuk upload binary dari antarmuka.
- `nas` - berbagi berkas penyimpanan HP di jaringan lokal bergaya SMB/FTP.
- `website` - hosting situs kecil dari HP dengan engine `static`, `node`, atau `php`, masing-masing dengan port dan tampilan log sendiri.
- `cron` - manajer job cron dengan backend host cronie dan scheduler fallback Go murni, termasuk log per job dan dry-run.
- `store` - front end paket Termux dengan katalog health-check dan pengelolaan mirror.
- `database` - jelajah berkas SQLite, daftar tabel, lihat skema, dan jalankan query melalui engine SQLite WASM bawaan.
- `backup` - ekspor dan impor seluruh konfigurasi Termux Monitor, plus kanal sinkronisasi cloud opsional.

### Workspace
- `files` - file manager dengan navigasi, upload, download, copy, move, batch rename, kompres, ekstrak, edit permission, dan pencarian.
- `logs` - penampil log aplikasi serta stream `logcat` langsung.
- `terminal` - PTY interaktif melalui WebSocket.
- `scrcpy` - mirroring layar dan penerusan input yang dirender dengan WebCodecs (wajib endpoint HTTPS).
- `tools` - ekspor backup konfigurasi dan pembantu perawatan.
- `about` - versi, status modul, dan informasi pembaruan.

---

## Instalasi

### Dari zip rilis

1. Unduh `Termux-Monitor-Magisk-v1.3.0.zip`.
2. Buka Magisk / KernelSU / APatch lalu pilih "Install from storage".
3. Pilih zip tersebut dan restart perangkat setelah proses instalasi selesai.
4. Modul langsung menjalankan daemon. Bila gagal, `service.sh` akan menjalankannya setelah boot.

### Dari Termux (manual)

```sh
pkg install golang
git clone https://github.com/sainz1407/termux-monitor.git
cd termux-monitor
go build -ldflags "-s -w" -o webui .
sudo ./webui --port 8080
```

Flag `--port` dan variabel lingkungan `PORT` sama-sama mengatur listener HTTP. `--https-port` (atau `HTTPS_PORT`) mengatur TLS; isi `0` untuk menonaktifkannya.

### Flag runtime

| Flag | Default | Fungsi |
| :--- | :--- | :--- |
| `--port` | `80` | Port HTTP |
| `--https-port` | `443` | Port HTTPS, `0` menonaktifkan TLS |
| `--apply-tweaks` | `false` | Terapkan tuning jaringan dan kernel tersimpan lalu keluar |

`service.sh` memanggil `webui --apply-tweaks` setiap boot agar tuning yang tersimpan tetap berlaku setelah reboot.

---

## Login Pertama

1. Buka `http://<ip-hp>:80` dari jaringan yang sama.
2. Masuk dengan password default `tmon`.
3. Segera ubah password dari bagian About atau Account.

Sesi memakai cookie `tmon_session`. Semua endpoint `/api/*` wajib sesi; hanya bundle statis dan endpoint login yang publik. Password dapat ditentukan sejak awal melalui `TMON_PASSWORD` atau diubah dari UI, yang menyimpan `auth.json` di direktori modul.

---

## Konfigurasi

Variabel lingkungan yang dibaca saat start:

| Variabel | Default | Fungsi |
| :--- | :--- | :--- |
| `TMON_PASSWORD` | `tmon` | Password admin awal |
| `PORT` | `80` | Override port HTTP |
| `HTTPS_PORT` | `443` | Override port HTTPS |
| `TMON_SU_BIN` | `su` | Binary helper root (`su`, `ksu`, `apatch`, atau path absolut) |
| `TMON_MODULE_DIR` | `/data/adb/modules/termux_monitor` | Root modul untuk berkas state |
| `TMON_ALLOWED_DIRS` | `/sdcard,/storage,/data/adb,/data/local/tmp,/data/system` | Root yang diizinkan untuk file manager |
| `TMON_SMS_DB` | deteksi otomatis | Override path `mmssms.db` milik ROM |
| `TMON_LEASES_FILE` | `/data/misc/dhcp/dnsmasq.leases` | Berkas lease untuk pembacaan klien hotspot |

Konfigurasi persisten disimpan di samping binary dalam bentuk berkas JSON, misalnya `network_config.json`, `kernel_config.json`, `charger_config.json`, `ssh_config.json`, `telegram_config.json`, `nas.json`, `tunnel.json`, dan `power_schedule.json`. Installer Magisk mempertahankan seluruh berkas konfigurasi yang dikenal saat pembaruan.

---

## Kebutuhan Build

| Alat | Versi yang dipakai |
| :--- | :--- |
| Go | 1.26 atau lebih baru |
| Node.js | 22 atau lebih baru |
| npm | 10 atau lebih baru (`bun` opsional) |
| Python | 3.9 atau lebih baru, hanya untuk `scripts/audit_nomodem.py` |

Versi dependensi frontend dipin di `frontend/package.json`: Svelte 5.56, Vite 8, TypeScript 6 dan Tailwind CSS 3.

---

## Build dari Sumber

### 1. Build frontend

```sh
cd frontend
npm install
npm run build
cd ..
```

Hasil build ditulis ke direktori aset yang di-embed, sehingga build Go harus selalu dijalankan sesudahnya.

### 2. Build binary Android

Windows (PowerShell):

```powershell
$env:GOOS="android"; $env:GOARCH="arm64"; $env:CGO_ENABLED="0"
go build -tags nomodem -ldflags "-s -w" -o webui .
```

Linux / Termux / macOS:

```sh
GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "-s -w" -o webui .
```

### 3. Paket menjadi zip Magisk

Windows:

```powershell
.\build-release.bat
```

Linux / Termux:

```sh
./build.sh              # binary saja
make release            # dua zip flavor
```

`build-release.bat` dan `make release` menghasilkan:

- `Termux-Monitor-Magisk-v1.3.0.zip`
- `Termux-Monitor-Magisk-v1.3.0-nomodem.zip`

Setiap zip berisi `META-INF/`, `customize.sh`, `module.prop`, `service.sh`, `system.prop`, `update.json`, `env.example`, `uninstall.sh`, `bin/`, dan binary `webui`.

---

## Flavor Build

Kedua flavor dikompilasi dari sumber yang identik.

| Flavor | Perintah | Catatan |
| :--- | :--- | :--- |
| Full | `go build ./...` | Rilis standar |
| No-modem | `go build -tags nomodem ./...` | Tag `nomodem` adalah alias no-op agar CI dan packaging tetap identik |

Perkakas baseband proprietary (QRTR/DIAG/EFS2, helper RIL vendor) sudah tidak ada di codebase. `internal/modem` hanya mengekspos telemetri yang tersedia di semua perangkat Android. `scripts/audit_nomodem.py` memindai binary hasil build terhadap sisa string vendor dan wajib melaporkan PASS untuk kedua flavor.

---

## Checklist Verifikasi

Jalankan rangkaian berikut sebelum membuka pull request atau menerbitkan rilis:

```sh
GOOS=android GOARCH=arm64 go build ./...
GOOS=android GOARCH=arm64 go build -tags nomodem ./...

go vet ./...
go vet -tags nomodem ./...

go test ./...
go test -tags nomodem ./...

cd frontend && npx svelte-check --tsconfig ./tsconfig.app.json
cd frontend && npm run build

python scripts/audit_nomodem.py webui
python scripts/audit_nomodem.py webui-nomodem
```

Binary test hasil cross-compile tidak dapat dieksekusi di host desktop; jalankan `go test` secara native untuk suite unit test.

---

## Struktur Proyek

```text
main.go                     Entry point, flag, server HTTP/HTTPS, manager latar belakang
internal/auth/              Manajer sesi dan penanganan cookie
internal/backup/            Ekspor, impor, dan sinkronisasi cloud konfigurasi
internal/charger/           Pengatur batas arus/tegangan pengisian
internal/cron/              Backend host cronie dan scheduler fallback Go
internal/database/          Layanan SQLite berbasis ncruces/go-sqlite3 (WASM)
internal/filemanager/       Navigasi berkas, arsip, dan helper transfer
internal/handlers/          Handler HTTP dan pendaftaran route per fitur
internal/hotspot/           Kontrol SoftAP, pembacaan klien, sinkronisasi router
internal/kernel/            Tuning governor, cluster, dan VM
internal/logger/            Logging terstruktur
internal/modem/             Telemetri modem read-only
internal/modules/           Deteksi dan instalasi modul Magisk/KernelSU
internal/nas/               Layanan berbagi berkas di jaringan lokal
internal/network/           Tuning sysctl/DNS/TTL dari network_config.json
internal/power/             Aksi daya dan penjadwal
internal/procmon/           Daftar proses berjalan, deteksi runtime, dan hentikan aman
internal/scrcpy/            Hub mirroring layar, packetisasi AVCC, kanal input
internal/smsviewer/         Pengambilan dan parsing SMS
internal/speedtest/         Runner pengujian throughput
internal/ssh/               Kontrol Dropbear
internal/store/             Store paket Termux, katalog, pengelolaan mirror
internal/sysinfo/           Penghitung /proc
internal/telegram/          Control plane bot Telegram
internal/terminal/          Terminal PTY WebSocket
internal/tlsgen/            Pembuatan sertifikat self-signed
internal/tunnel/            Pengelolaan engine tunnel
internal/version/           Metadata versi
internal/vnstat/            Pembungkus pencatatan bandwidth
internal/website/           Hosting situs static/node/php
internal/worker/            Scheduler berkala latar belakang
frontend/                   SPA Svelte 5, ditanam melalui frontend/embed.go
scripts/audit_nomodem.py    Guard regresi zero-leak
```

---

## HTTP API

Seluruh route di bawah membutuhkan cookie sesi yang sudah terautentikasi kecuali disebutkan lain.

| Grup | Endpoint |
| :--- | :--- |
| Auth | `POST /api/auth/login`, `POST /api/auth/logout`, `GET /api/auth/status`, `POST /api/auth/change-password` |
| Sistem | `GET /api/sysinfo`, `GET /api/stats`, `POST /api/power`, `GET /api/logs`, `POST /api/logs/clear` |
| Proses | `GET /api/processes`, `POST /api/processes/kill` |
| Berkas | `/api/files/` list, read, save, upload, download, mkdir, create, delete, copy, move, batch, compress, extract, permissions, search, storage |
| Jaringan | `/api/network/` dns, dns/reset, ttl, ping; `/api/hotspot/` status, control, clients, settings |
| Modem | `/api/modem/signal`, `/api/modem/thermal`, `/api/modem/cell_scan`, `/api/modem/sim/status`, `/api/modem/sim/switch`, `GET /api/sms/inbox` |
| Kernel | `/api/kernel/` status, cluster, profile, vm; `/api/sysinfo/governor` |
| Store | `/api/store/` status, list, install, install/log, op, op/log, mirror |
| Cron | `/api/cron/` crontab, daemon, jobs, jobs/add, jobs/delete, jobs/run, jobs/toggle, dry-run, runs, service, download |
| Website | `/api/website/` list, probe, add, start, stop, restart, delete, logs |
| Database | `/api/database/` status, ls, tables, schema, query |
| Layanan | `/api/ssh/*`, `/api/telegram/*`, `/api/tunnel/*`, `/api/nas/*`, `/api/speedtest/*`, `/api/vnstat/*`, `/api/backup/*` |
| WebSocket | `/api/terminal/ws`, `/api/scrcpy/ws`, `/api/logs/logcat/stream` |

Operasi berdurasi panjang (instalasi paket, operasi cron) mengembalikan handle dan mengalirkan progres melalui endpoint log pasangannya.

---

## Catatan Keamanan

- Panel ini ditujukan untuk LAN tepercaya atau tunnel privat. Jangan ekspos port 80 atau 443 langsung ke internet tanpa tunnel atau VPN.
- Segera ubah password default `tmon` setelah login pertama.
- TLS memakai sertifikat self-signed yang dibuat saat start pertama; peramban akan memperingatkan sampai sertifikat diterima atau di-pin. Mirroring `scrcpy` membutuhkan endpoint HTTPS karena WebCodecs memerlukan secure context.
- Perintah berhak khusus dijalankan lewat `su -c`; proses WebUI sendiri berjalan sebagai root dari direktori modul.
- Akses file manager dibatasi oleh `TMON_ALLOWED_DIRS`.
- `uninstall.sh` menghentikan daemon tetapi sengaja membiarkan konfigurasi JSON Anda, dan tidak mengembalikan perubahan sysctl. Reboot untuk memulihkan perilaku kernel dan jaringan bawaan.

---

## Penanganan Masalah

| Gejala | Tindakan |
| :--- | :--- |
| Panel tidak bisa diakses setelah instalasi | Periksa `/data/adb/modules/termux_monitor/webui.log`, pastikan modul aktif dan `service.sh` berjalan setelah boot |
| Port 80 terpakai | Jalankan `webui --port 8080` atau set variabel `PORT` |
| Login ditolak | Reset dengan mengedit `auth.json` di direktori modul, atau start ulang dengan `TMON_PASSWORD=tmon` |
| `scrcpy` menampilkan layar hitam | Buka panel lewat `https://<ip-hp>:443`, terima sertifikat, lalu muat ulang. Gunakan peramban berbasis Chromium versi terbaru |
| Operasi Store gagal | Instalasi paket membutuhkan DNS yang berfungsi di perangkat; tab `store` menampilkan penyebab gagal secara detail di tampilan log |
| Job cron tidak berjalan | UI menampilkan apakah daemon cronie atau scheduler fallback Go yang aktif; pastikan modul tidak dibatasi optimasi baterai |
| Tuning kembali normal setelah reboot | Pastikan `service.sh` menjalankan `webui --apply-tweaks` dan modul tidak dalam kondisi nonaktif |

---

## Kredit dan Lisensi

Dikembangkan dan dipelihara oleh `sainz1407`. Repositori: <https://github.com/sainz1407/termux-monitor>.

Komponen pihak ketiga: `gorilla/websocket`, `creack/pty`, `ncruces/go-sqlite3` (SQLite WASM), `golang.org/x/crypto`, `golang.org/x/net`, Svelte, Vite dan Tailwind CSS.

Dirilis di bawah GNU General Public License v3.0. Lihat `LICENSE`.
