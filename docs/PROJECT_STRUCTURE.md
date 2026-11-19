# Struktur Proyek Termux Monitor

Dokumen ini menjelaskan arsitektur folder dan struktur berkas repositori **Termux Monitor**, mencakup komponen Backend Go, Frontend Modern Svelte 5, serta komponen Modul Android (Magisk/KernelSU/APatch).

---

## 📁 Pohon Direktori Utama

```text
Termux Monitor/
├── main.go                      # Entry point aplikasi, inisialisasi server & flag CLI
├── module.prop                  # Metadata modul Android (id, name, version v1.3.0, author)
├── customize.sh                 # Script instalasi & setup izin saat di-flash via Root Manager
├── service.sh                   # Script startup otomatis saat booting perangkat Android
├── system.prop                  # System properties bawaan modul Android
│
├── .github/                     # GitHub Actions CI/CD Workflows
│   └── workflows/
│       ├── quality.yml          # Frontend check & Go unit test verification
│       ├── release.yml          # Automated release & Magisk zip packager
│       └── cleanup.yml          # Pembersihan paket rilis lama
│
├── docs/                        # Folder Dokumentasi Proyek
│   ├── INSTALLATION_ID.md       # Panduan Instalasi (Bahasa Indonesia)
│   ├── INSTALLATION_EN.md       # Panduan Instalasi (English)
│   ├── DEVELOPMENT.md           # Panduan Pengembangan & Kompilasi
│   ├── DESIGN.md                # Spesifikasi Desain UI/UX & Tema
│   ├── API_REFERENCE.md         # Referensi REST API & WebSocket
│   ├── PROJECT_STRUCTURE.md     # Dokumen Struktur Proyek (Dokumen Ini)
│   └── SECURITY.md              # Model ancaman & kontrol keamanan
│
├── internal/                    # Kode Sumber Backend (Go Internal Packages)
│   ├── auth/                    # Sesi login, Cookie SameSite=Strict, CSPRNG Token
│   ├── charger/                 # Kontrol sysfs pengisian daya baterai & auto-detect PMIC
│   ├── config/                  # Pengaturan Environment Variables terpusat (TMON_*) & atomic write
│   ├── cron/                    # Parser crontab, penjadwalan job, & riwayat eksekusi
│   ├── database/                # Browser berkas SQLite (tabel, skema, query runner)
│   ├── filemanager/             # Modul operasi berkas (ops, archive, search, perm)
│   ├── handlers/                # HTTP Routers & Modular Domain Handlers (routes_system, routes_network, routes_modem, routes_services)
│   ├── hotspot/                 # Kontrol SoftAP hotspot, tethering via binder, channel, & client ARP
│   ├── logger/                  # Stream log sistem & audit aktivitas real-time
│   ├── modem/                   # Telemetri seluler non-proprietary (signal, SIM, thermal, cell scan)
│   ├── modules/                 # Deteksi dan manajemen modul sistem Magisk/KSU/APatch
│   ├── nas/                     # Integrasi file sharing & WebDAV backup cloud sync
│   ├── network/                 # Tweaks sysctl, DNS resolver, RPS, MTU, & TTL dynamic
│   ├── power/                   # Eksekusi aksi daya (reboot, shutdown, bootloader, dll)
│   ├── procmon/                 # Pemantau proses langsung dari /proc (tanpa ps/top)
│   ├── backup/                  # Snapshot konfigurasi & sinkronisasi cloud backup
│   ├── kernel/                  # Profil kernel, governor, & tuning scheduler
│   ├── store/                   # Katalog paket/plugin store & status pembaruan
│   ├── tlsgen/                  # Generator sertifikat TLS self-signed untuk HTTPS
│   ├── version/                 # Resolver versi modul dari module.prop
│   ├── website/                 # Hosting website (node/php/static) & watchdog
│   ├── worker/                  # Scheduler tugas periodik latar belakang
│   ├── speedtest/               # Speedtest multi-thread native
│   ├── bufferpool/              # Pool buffer untuk streaming I/O efisien
│   ├── scrcpy/                  # Sesi Web Mirroring Layar Android & input touch
│   ├── smsviewer/               # Pembaca database SMS Android & inbox viewer
│   ├── ssh/                     # Kontrol daemon SSH / Dropbear
│   ├── sysinfo/                 # Polling hardware counters, CPU, RAM, Suhu, & Network
│   ├── telegram/                # Telegram bot remote management & command keyboards
│   ├── terminal/                # Interactive PTY Web Terminal via WebSocket (shell user Termux)
│   ├── tunnel/                  # Cloudflare / Cloudflared tunnel manager
│   └── vnstat/                  # Pemantauan penggunaan data lalu lintas jaringan proc
│
└── frontend/                    # Frontend Single Page Application (Svelte 5 + Vite)
    ├── bun.lock                 # Lockfile dependensi Bun (dipakai CI)
    ├── package-lock.json        # Lockfile npm (dipakai saat build lokal)
    ├── embed.go                 # Direktif go:embed untuk menanam dist/ ke biner Go
    ├── package.json             # Manifest dependensi & script frontend
    ├── svelte.config.js         # Konfigurasi compiler Svelte 5
    ├── tsconfig.json            # TypeScript configuration
    ├── index.html               # Entry point HTML Single Page Application
    │
    └── src/                     # Kode Sumber Frontend Svelte 5
        ├── App.svelte           # Root application shell & router view
        ├── main.ts              # Frontend mounting entry point
        │
        ├── api/                 # API Client layer
        │   └── client.ts        # Typed fetch client dengan interceptor & auth handler
        │
        ├── components/          # Komponen Svelte 5 Berbasis Kategori
        │   ├── layout/          # Header, Sidebar, BottomNav, Toasts, AppearanceModal
        │   ├── ui/              # Komponen Reusable (Card, Button, Badge, Modal, Input)
        │   └── tabs/            # Tab Views & registry.ts (Pemetaan Dinamis Komponen Tab)
        │       ├── overview/    # Tab Overview & CasaOS Application Shortcuts
        │       ├── sysinfo/     # Tab System Info, Hardware Sensors, CPU Cores, Root Daemons
        │       ├── network/     # Tab Network Tweaks, DNS, TTL, MTU, Interfaces
        │       ├── vnstat/      # Tab Vnstat Bandwidth Accounting & Quota
        │       ├── modem/       # Tab Cellular Telemetry (Signal, SIM, Thermal)
        │       ├── hotspot/     # Tab Tethering Suite (Channels, Clients & L3 Router)
        │       ├── files/       # Modular Dual-Pane File Manager
        │       │   ├── types.ts            # Kontrak interface data murni
        │       │   ├── paneState.svelte.ts # Svelte 5 Runes State Manager per-panel
        │       │   ├── BookmarksBar.svelte # Quick-jump preset & pin kustom localStorage
        │       │   ├── FileDropzone.svelte # Overlay drag & drop upload terpadu
        │       │   ├── FileTable.svelte    # Tabel file responsif & aksi baris
        │       │   ├── FileModals.svelte   # Konsolidasi 12 modal dialog terpusat
        │       │   ├── FilePane.svelte     # Kontainer panel mandiri (breadcrumbs, toolbar)
        │       │   └── TabFileManager.svelte # Koordinator Single/Dual Pane & mobile tab switcher
        │       ├── terminal/    # Tab Web Terminal PTY xterm.js
        │       ├── scrcpy/      # Tab Android Screen Mirroring & Controls
        │       ├── charger/     # Tab Battery Charging Limiter & PMIC Controller
        │       ├── ssh/         # Tab Dropbear SSH Server Manager
        │       ├── sms/         # Tab SMS Inbox Reader & AT Commands
        │       ├── nas/         # Tab LAN Web Share & NAS File Server
        │       ├── tunnel/      # Tab Cloudflare / Cloudflared Tunnel
        │       ├── logs/        # Tab Live Daemon Logs & Logcat Stream
        │       ├── modules/     # Tab Magisk / KernelSU Module Manager Resmi
        │       ├── tools/       # Tab Peralatan Sistem & Snapshot Backup
        │       ├── telegram/    # Tab Bot Telegram Remote Management
        │       ├── speedtest/   # Tab Native Multi-Thread Speedtest
        │       ├── about/       # Tab About, Key Features, & Tautan Proyek
        │       ├── kernel/      # Tab Kernel Profile & Governor
        │       ├── store/       # Tab Store Paket & Pembaruan Plugin
        │       ├── cron/        # Tab Crontab Manager, Job, & Riwayat Run
        │       ├── website/     # Tab Hosting Website & Watchdog
        │       ├── database/    # Tab Browser SQLite (Tabel, Skema, Query)
        │       ├── power/       # Tab Power Actions & Jadwal Reboot
        │       └── processes/   # Tab Process Monitor (/proc) & Aksi Stop
        │
        ├── stores/              # Svelte 5 Rune Reactive Stores
        │   ├── auth.svelte.ts   # State otentikasi sesi & token
        │   ├── navigation.svelte.ts # State tab aktif, kategori, & hash routing
        │   ├── theme.svelte.ts  # AMOLED Dark / Light theme switcher
        │   └── toast.svelte.ts  # Global notification toast store
        │
        └── styles/              # Global Styles
            └── app.css          # Tailwind directives & Neo-Brutalist utility classes
```
