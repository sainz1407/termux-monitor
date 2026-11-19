# CLAUDE.md - Project Rules & Guidelines for Claude Code

## 1. Project Overview
`Termux Monitor` is an ultra-lightweight, 100% offline-ready Android System Control Panel & WebUI designed to run as a Magisk / KernelSU / APatch module. It is written in modular Go and uses Svelte 5 + TypeScript + Tailwind CSS for the frontend, all embedded inside a single binary (~10-12MB) with a tiny memory footprint.

---

## 2. Codebase Structure
- `main.go`: Entry point, command line flags, server setup, background workers.
- `internal/auth/`: Cookie-based session authentication manager & password hashing.
- `internal/backup/`: Configuration export/import and cloud sync.
- `internal/bufferpool/`: Reusable byte buffers for hot read paths.
- `internal/charger/`: Hardware-specific charging limitation controllers & sysfs scanners.
- `internal/config/`: Persistent data directory and atomic file writes.
- `internal/cron/`: Crontab parsing, job scheduling and run history.
- `internal/database/`: SQLite file browser: tables, schema and query runner.
- `internal/filemanager/`: File navigation, download, upload, and archive management.
- `internal/handlers/`: Web controllers and API request routers.
- `internal/hotspot/`: SoftAP controls, MAC filtering, connected clients ARP parsing, and the managed L3 router engine (gateway, subnet, DHCP, DNS, upstream).
- `internal/kernel/`: Kernel/VM tuning profiles, cluster and governor control.
- `internal/logger/`: Centralized structured logging.
- `internal/modem/`: Telemetry-only cellular engine (signal, SIM status, SIM switch, thermal, cell scan).
- `internal/modules/`: System module detection and command executions.
- `internal/nas/`: Local WebDAV & HTTP media sharing server.
- `internal/network/`: Persistent sysctl values, DNS options, dynamic SDK-aware TTL configurations.
- `internal/power/`: System power actions (`su -c reboot` wrapper).
- `internal/scrcpy/`: Scrcpy web integration, rendering and input event channels.
- `internal/smsviewer/`: AT-based/modem SMS parsing and retrieval handlers.
- `internal/speedtest/`: Multi-worker Go speedtest engine with ISP/Colo detection.
- `internal/ssh/`: Dropbear SSH client configuration and state manager.
- `internal/store/`: Package/plugin store catalog and update state.
- `internal/sysinfo/`: Hardware counters `/proc` reading utilities & per-core CPU usage.
- `internal/telegram/`: Bidirectional Telegram Bot daemon & SMS auto-forwarder.
- `internal/terminal/`: WebSocket interactive PTY terminal.
- `internal/tlsgen/`: Self-signed TLS certificate generation.
- `internal/tunnel/`: Persistent ARM64 daemon remote access tunnel (`cloudflared`, `tailscaled`, `zerotier-one`).
- `internal/vnstat/`: Vnstat bandwidth logging and analysis wrapper.
- `internal/website/`: Website hosting status and watchdog.
- `internal/worker/`: Periodic background task scheduler and cron events.
- `frontend/`: Modern Svelte 5 Single Page Application (embedded via `frontend/embed.go`).

---

## 3. Strict Routing & Verification Rules
- **UI Architecture**: Supports modular UI paradigms (Neobrutal & Modern Clean). Follow design instructions strictly without breaking CSS scoping (`[data-ui-style="..."]`).
- **Cross-Compilation Verification**: Always verify code modifications compile cleanly targeting `GOOS=android GOARCH=arm64` with zero syntax/type errors (`go test ./...` and `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go test -c ./...`).
- **Tuning Config Isolation**: System-level network tweaks must be driven by variables defined within `network_config.json` and resolved in `internal/network/config.go` dynamically. Never hardcode network config values or execute raw sysctl commands outside these properties.

---

## 4. Agent Behavior & Operational Constraints
- **Strict User Consent & Autonomy**: Adhere strictly to rules in `AGENTS.md` and `CLAUDE.md`. MUST NOT initiate any code modifications, refactoring, or file creations without explicit user requests.
- **Git Operations**: **DO NOT** run `git commit` or `git push` commands unless explicitly requested by the user.
- **No Arbitrary Code Editing**: Do not make arbitrary or unsolicited code modifications.
- **Language & Communication**:
  - Code, inline comments, commit messages, and configurations MUST be written in **English**.
  - ALL direct interaction and communication with the user MUST be conducted in **Indonesian (Bahasa Indonesia)**.
- **Code Length & Modularization**: If any single file contains code that is too long or complex, recommend splitting it into modular files rather than expanding it further.
- **Handling UI Feature Additions / Tabs**:
  - If the user requests a new UI feature or an addition to an existing tab, the agent **MUST** explicitly ask the user whether the feature should be created as a new dedicated tab or added to an existing tab.
  - Provide a recommendation with pros and cons for both options before the user decides.
- **Handling Inquiries, Analyses, and Feature Requests**:
  - If a user request involves analysis, proposes a new feature, ends with a "?" (indicating a question or exploratory intent), or has a tone representing an inquiry or questioning command, the agent **MUST NOT** directly modify the codebase.
  - Present clear recommendations or design candidates, detailing the pros, cons, and trade-offs of each option, and await explicit design/implementation approval from the user.

---

## 5. Modem Telemetry & Build Flavor Rules (Dual-Flavor Distribution)
- **Vendor stack retired**: All proprietary baseband tooling (Qualcomm QRTR/DIAG/EFS2, Samsung SecRIL, Google Tensor Shannon) has been **removed entirely** from the codebase. There is no `internal/efs`, `internal/samsung`, or `internal/shannon`, no `routes_vendor_modem.go`, and no `/api/{efs,qualcomm,shannon,samsung}/*` endpoints.
- **Telemetry-only modem core**: `internal/modem` retains **non-proprietary telemetry only** — signal, SIM status, SIM switch (`service call isub 31`, pure native IPC), thermal, and cell scan. Never reintroduce AT commands, QRTR/PDC, band locking, or cell/PLMN locking.
- **Build flavors**: Both `dev` and `release/no-modem` build from **identical sources**. The `nomodem` build tag is kept as a no-op alias so `go build -tags nomodem ./...` stays green; it no longer gates any vendor code.
  - Full build: `GOOS=android GOARCH=arm64 go build ./...`
  - No-modem build: `GOOS=android GOARCH=arm64 go build -tags nomodem ./...`
  - Frontend is shared: **never** write per-flavor `features.ts` / `vendor_tabs.ts` files. The `HAS_VENDOR_MODEM` gate and `vendor_tabs.ts` seam no longer exist; any new vendor UI gating must be added only if proprietary code is reintroduced (which requires explicit approval).
- **Zero-leak regression guard**: `scripts/audit_nomodem.py` scans the built binary for residual vendor paths/endpoints. Keep it passing in both flavors.
