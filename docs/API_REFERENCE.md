# Termux Monitor API Reference

This document provides complete documentation for the REST API endpoints and WebSocket endpoints exposed by the **Termux Monitor** system control panel.

---

## 🔒 Authentication & Security

All API endpoints (except `/api/auth/login` and `/api/auth/status`) require session authentication.
- **Session Cookie**: Set upon login (`SameSite=Strict`, `HttpOnly`).
- **Security Headers**: All responses include `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, and `Referrer-Policy: no-referrer`.
- **Body Limit**: POST/PUT requests enforce a 1MB payload limit via `http.MaxBytesReader` (except file uploads which permit up to 32MB).

---

## 🔑 Authentication Endpoints

### `POST /api/auth/login`
Authenticates a user session.

- **Request Body**:
  ```json
  {
    "password": "your_password"
  }
  ```
- **Response** (`200 OK`):
  ```json
  {
    "success": true
  }
  ```

### `GET /api/auth/status`
Checks if the current session is authenticated.

- **Response** (`200 OK`):
  ```json
  {
    "authenticated": true
  }
  ```

### `POST /api/auth/logout`
Clears the current authentication cookie.

---

## 📊 System Information Endpoints

### `GET /api/sysinfo` (or `/api/stats`)
Returns full hardware metrics, CPU/RAM usage, temperatures, network details, and active services.

- **Response** (`200 OK`):
  ```json
  {
    "cpu_usage": 12.5,
    "cpu_cores": [10.0, 15.0, 8.0, 14.0],
    "cpu_temp": 38.5,
    "mem_total": 7800000000,
    "mem_used": 3400000000,
    "mem_free": 200000000,
    "mem_available": 4200000000,
    "mem_used_pct": 43.5,
    "battery_level": 85,
    "battery_temp": 31.0,
    "active_services": [
      {
        "name": "Web UI Server",
        "key": "webui",
        "running": true,
        "detail": "Running (Port 80)",
        "pid": 21800,
        "cpu": 0.2,
        "ram": 20.7
      },
      {
        "name": "SSH Daemon",
        "key": "ssh",
        "running": true,
        "detail": "Running (Port 22)",
        "pid": 25140,
        "ram": 0.05
      }
    ],
    "network_detail": {
      "ip_addresses": ["192.168.100.55"],
      "gateway": "192.168.100.1",
      "dns": ["1.1.1.1 (Active)", "8.8.8.8 (Active)"],
      "dns1": "1.1.1.1 (Active)",
      "dns2": "8.8.8.8 (Active)",
      "wifi_ssid": "TOTOLINK_X6000R_5G",
      "wifi_signal": "-38 dBm / 866Mbps",
      "hotspot_clients": 2
    }
  }
  ```

---

## 📁 File Manager Endpoints

### `GET /api/files/list?path={directory_path}`
Lists directory entries and returns storage metrics.

- **Response** (`200 OK`):
  ```json
  {
    "path": "/sdcard",
    "files": [
      {
        "name": "Download",
        "path": "/sdcard/Download",
        "is_dir": true,
        "size": 4096,
        "mod_time": "2026-08-01T12:00:00Z",
        "permissions": "drwxrwxr-x"
      }
    ]
  }
  ```

### `GET /api/files/read?path={file_path}`
Reads file text contents (max 5MB).

### `POST /api/files/save`
Saves text content to a file.

- **Request Body**:
  ```json
  {
    "path": "/sdcard/config.json",
    "content": "{ \"key\": \"value\" }"
  }
  ```

### `POST /api/files/upload`
Uploads a file using `multipart/form-data`.
- Form Fields: `path` (destination directory), `file` (binary payload).

### `GET /api/files/download?path={file_path}`
Downloads a file as `application/octet-stream`.

### `POST /api/files/copy` & `POST /api/files/move`
Copies or moves a file/folder recursively.

- **Request Body**:
  ```json
  {
    "src": "/sdcard/source.txt",
    "dst": "/sdcard/Download/source.txt"
  }
  ```

### `POST /api/files/batch`
Performs bulk operations on multiple items.

- **Request Body**:
  ```json
  {
    "action": "delete | copy | move",
    "paths": ["/sdcard/a.txt", "/sdcard/b.txt"],
    "dest_dir": "/sdcard/Download"
  }
  ```

### `POST /api/files/permissions`
Modifies file octal permissions (`chmod`) and ownership (`chown`).

- **Request Body**:
  ```json
  {
    "path": "/data/local/tmp/script.sh",
    "mode": "0755",
    "owner": "root:root"
  }
  ```

### `POST /api/files/compress` & `POST /api/files/extract`
Compresses paths into a `.zip` file or extracts an existing ZIP file.

- **Compress Request Body**:
  ```json
  {
    "paths": ["/sdcard/folder1", "/sdcard/file.txt"],
    "dest_zip": "/sdcard/archive.zip"
  }
  ```

### `GET /api/files/search?path={root_path}&query={keyword}`
Searches directory recursively for matching files/directories (capped at 500 items).

### `GET /api/files/storage`
Returns system storage metrics.

- **Response**:
  ```json
  {
    "total": 128000000000,
    "free": 64000000000,
    "used": 64000000000,
    "percent": 50.0,
    "mount": "/sdcard",
    "total_str": "119.2 GB",
    "free_str": "59.6 GB",
    "used_str": "59.6 GB"
  }
  ```

---

## 🌐 Network & Tweaks Endpoints

### `GET /api/network/dns`
Returns the active primary and secondary DNS resolvers along with preconfigured DNS provider presets.

- **Response** (`200 OK`):
  ```json
  {
    "primary": "1.1.1.1",
    "secondary": "1.0.0.1",
    "presets": [
      {
        "name": "Cloudflare",
        "primary": "1.1.1.1",
        "secondary": "1.0.0.1"
      },
      {
        "name": "Google",
        "primary": "8.8.8.8",
        "secondary": "8.8.4.4"
      }
    ]
  }
  ```

### `POST /api/network/dns`
Configures DNS servers on interface resolvers and `iptables` port 53 redirection.

- **Request Body**:
  ```json
  {
    "primary": "1.1.1.1",
    "secondary": "8.8.8.8"
  }
  ```

### `GET /api/network/ttl`
Returns current TTL spoofing status and default system TTL recommendation.

- **Response** (`200 OK`):
  ```json
  {
    "ttl_spoof": true,
    "current_ttl": 65,
    "ttl": 65
  }
  ```

### `POST /api/network/ttl`
Enables or disables TTL spoofing rules via iptables mangle chain.

- **Request Body**:
  ```json
  {
    "enable": true,
    "ttl": 65
  }
  ```

### `POST /api/network/ping`
Runs ICMP ping diagnostic to a host.

- **Request Body**:
  ```json
  {
    "host": "1.1.1.1",
    "count": 4
  }
  ```

---

## ⚡ Power Management Endpoints

### `POST /api/power`
Triggers power actions asynchronously.

- **Request Body**:
  ```json
  {
    "action": "reboot | shutdown | soft_reboot | recovery | bootloader"
  }
  ```

---

## ⚙️ Process Monitor Endpoints

### `GET /api/processes`
Lists the running processes, read directly from `/proc`. The optional `q` parameter filters by process name, command line or runtime, `sort` selects the ordering (`cpu`, `mem`, `pid`, `name`, `elapsed`) and `limit` caps the number of entries.

- **Query Parameters**: `q` (search term), `sort` (default `cpu`), `limit` (0 = no limit)
- **Response** (`200 OK`):
  ```json
  {
    "processes": [
      {
        "pid": 753,
        "ppid": 1,
        "name": "surfaceflinger",
        "cmd": "/system/bin/surfaceflinger",
        "cpu": 1.4,
        "mem_pct": 0.8,
        "mem_rss": 67108864,
        "mem_text": "64.0 MB",
        "state": "sleeping",
        "user": "system",
        "exe_path": "/system/bin/surfaceflinger",
        "runtime": "Native",
        "elapsed": "2h 14m",
        "elapsed_sec": 8040.5,
        "threads": 12
      }
    ],
    "count": 1,
    "sort": "cpu"
  }
  ```

The `runtime` field reports the interpreter behind the process: `Python`, `Node.js`, `Java`, `PHP`, `Ruby`, `Perl`, `Lua`, `.NET`, `Shell`, `Go`, `Termux`, `OpenSSH`, `Kernel` (kernel threads), `Native` (compiled binaries) or `Unknown`.

### `POST /api/processes/kill`
Stops a process. With `force` set to `false` the daemon sends `SIGTERM` first and escalates to `SIGKILL` when the process does not exit within the grace period; with `force` set to `true` the process is killed immediately with `SIGKILL`.

- **Request Body**:
  ```json
  {
    "pid": 4242,
    "force": false
  }
  ```
- **Response** (`200 OK`):
  ```json
  {
    "success": true,
    "error": ""
  }
  ```

The daemon refuses to terminate PID 1, itself and its own parent process.

---

## 💻 Terminal & Scrcpy WebSockets

- `GET /api/terminal/ws`: Interactive PTY terminal session over WebSocket. The shell runs as the Termux user so `pkg` and the other Termux tools work; type `su` to escalate to root.
- `GET /api/scrcpy/ws`: Android screen streaming & touch input over WebSocket.

---

## 📚 Endpoint Index

The sections above document the core endpoints in detail. The remaining routes are grouped per module below. Every route requires an authenticated session, read endpoints use `GET`, and mutating endpoints use `POST`.

| Module | Endpoints |
| :--- | :--- |
| Authentication | `/api/auth/login`, `/api/auth/logout`, `/api/auth/status`, `/api/auth/change-password` |
| System Info | `/api/sysinfo`, `/api/stats`, `/api/sysinfo/governor`, `/api/protected` |
| Processes | `/api/processes`, `/api/processes/kill` |
| Kernel | `/api/kernel/status`, `/api/kernel/profile`, `/api/kernel/cluster`, `/api/kernel/vm` |
| Logs | `/api/logs`, `/api/logs/clear`, `/api/logs/logcat/stream` |
| Tethering | `/api/hotspot/status`, `/api/hotspot/control`, `/api/hotspot/settings`, `/api/hotspot/clients` |
| Cellular | `/api/modem/signal`, `/api/modem/sim/status`, `/api/modem/sim/switch`, `/api/modem/thermal`, `/api/modem/cell_scan` |
| Network | `/api/network/dns`, `/api/network/dns/reset`, `/api/network/ping`, `/api/network/ttl` |
| Vnstat | `/api/vnstat/stats`, `/api/vnstat/reset` |
| SSH | `/api/ssh/status`, `/api/ssh/config`, `/api/ssh/control` |
| Charger | `/api/charger/probe`, `/api/charger/config`, `/api/charger/toggle` |
| Power | `/api/power`, `/api/power/action`, `/api/power/schedule` |
| Modules | `/api/modules`, `/api/modules/list`, `/api/modules/install`, `/api/modules/toggle` |
| NAS | `/api/nas/status`, `/api/nas/start`, `/api/nas/stop` |
| Website Hosting | `/api/website/list`, `/api/website/add`, `/api/website/delete`, `/api/website/start`, `/api/website/stop`, `/api/website/restart`, `/api/website/probe`, `/api/website/logs` |
| Store | `/api/store/list`, `/api/store/status`, `/api/store/mirror`, `/api/store/op`, `/api/store/op/log`, `/api/store/install`, `/api/store/install/log` |
| Cron | `/api/cron/crontab`, `/api/cron/jobs`, `/api/cron/jobs/add`, `/api/cron/jobs/delete`, `/api/cron/jobs/toggle`, `/api/cron/jobs/run`, `/api/cron/jobs/dry-run`, `/api/cron/runs`, `/api/cron/daemon`, `/api/cron/service`, `/api/cron/download` |
| Database | `/api/database/ls`, `/api/database/tables`, `/api/database/schema`, `/api/database/query`, `/api/database/status` |
| Tunnel | `/api/tunnel/status`, `/api/tunnel/start`, `/api/tunnel/stop`, `/api/tunnel/upload` |
| Telegram | `/api/telegram/status`, `/api/telegram/config`, `/api/telegram/control` |
| SMS | `/api/sms/inbox` |
| Speedtest | `/api/speedtest/start`, `/api/speedtest/stop`, `/api/speedtest/status`, `/api/speedtest/history` |
| Backup | `/api/backup/export`, `/api/backup/import`, `/api/backup/cloud/config`, `/api/backup/cloud/sync` |
| Shortcuts | `/api/shortcuts/list`, `/api/shortcuts/save`, `/api/shortcuts/delete` |
| Theme | `/api/theme/wallpapers`, `/api/theme/wallpapers/upload`, `/api/theme/wallpapers/delete`, `/api/theme/wallpapers/file` |
