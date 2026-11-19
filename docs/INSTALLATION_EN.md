# Termux Monitor Installation Guide (English)

This document provides a step-by-step guide to installing and running **Termux Monitor** on root-accessed Android devices (using Magisk, KernelSU, or APatch).

---

## 📋 System Requirements

1. Rooted Android device (Android 8.0+ recommended).
2. One of the following Root Managers:
   - **Magisk** v20.4+
   - **KernelSU** v0.6.0+
   - **APatch** v10.7+
3. At least ~25MB of free storage space.

---

## 🚀 Installation Methods

### Method 1: Installing via Modul Zip (Recommended)

1. Download the latest module zip release package `Termux-Monitor-Magisk-vX.Y.Z.zip` from the [Releases](https://github.com/sainz1407/termux-web/releases) page.
2. Open your Root Manager application (**Magisk App**, **KernelSU App**, or **APatch App**).
3. Go to the **Modules** / **Modul** section.
4. Select **Install from storage** and choose the downloaded zip file.
5. Wait for the flashing process to complete, then **Reboot** your device.

---

### Method 2: Manual Installation via Root Terminal / Shell

If you wish to test or install the binary manually without wrapping it into a zip package:

1. Copy the `webui` binary to the modules directory:
   ```bash
   su
   mkdir -p /data/adb/modules/termux_monitor
   cp /sdcard/Download/webui /data/adb/modules/termux_monitor/
   chmod 755 /data/adb/modules/termux_monitor/webui
   ```

2. Create a basic `module.prop` metadata file:
   ```bash
   cat << 'EOF' > /data/adb/modules/termux_monitor/module.prop
   id=termux_monitor
   name=Termux Monitor
   version=v1.3.0
   versionCode=130
   author=sainz1407
   description=Termux System Status WebUI &#38; Control Panel built in Go &#38; Svelte 5.
   EOF
   ```

3. Create the boot service script `service.sh`:
   ```bash
   cat << 'EOF' > /data/adb/modules/termux_monitor/service.sh
   #!/system/bin/sh
   MODDIR=${0%/*}
   until [ "$(getprop sys.boot_completed)" = "1" ]; do
       sleep 2
   done
   sleep 5
   $MODDIR/webui > /dev/null 2>&1 &
   EOF
   chmod 755 /data/adb/modules/termux_monitor/service.sh
   ```

4. Launch the boot script manually or reboot:
   ```bash
   /data/adb/modules/termux_monitor/service.sh &
   ```

---

## 🌐 Accessing the WebUI

1. Ensure your device is connected to a local Wi-Fi network or use localhost network.
2. Open any web browser (Chrome, Kiwi, Firefox, etc.) on your Android device or another device on the same local network.
3. Access the URL:
   ```text
   http://localhost:80
   ```
   *Or from another device in the same local network:*
   ```text
   http://<YOUR_ANDROID_IP>:80
   ```
4. Enter the default password on login:
   ```text
   tmon
   ```

### 🎥 Screen Mirror Note (Scrcpy)

The screen mirror uses **WebCodecs H.264**, which is only available in a *secure context* (HTTPS or
`localhost`). When the WebUI is opened through `http://<YOUR_ANDROID_IP>:80` from another device, the
browser exposes no H.264 decoder, so the app automatically switches to the **MJPEG (screencap ~3 FPS)**
mode — the screen still renders and touch/remote control keeps working, only at a lower frame rate.

For the full speed path (H.264 at 60 FPS) use one of the following:
- Open the WebUI through `http://localhost:80` in the browser on the Android device itself.
- Use the HTTPS endpoint and install the module certificate
  (`/data/adb/modules/termux_monitor/tls/cert.pem`) as a trusted CA on the client device.

---

## 🌐 Remote Access Tunnel

The **Tunnel** tab exposes the WebUI to the internet without any port forwarding.

1. Pick an engine: **Cloudflare** (free quick tunnel), **Tailscale**, **ZeroTier**, **Ngrok** or **Pinggy**.
2. For Cloudflare, leave the token empty to use the free *quick tunnel*, or fill in the
   `cloudflared tunnel run --token ...` token for a named tunnel.
3. Click **Start Tunnel**. The public URL shows up after roughly 10 seconds and is also
   written to the log panel.

The engine binary is looked up in the module data directory, in `PATH`, in
`/data/adb/modules/*/bin` and in the Termux prefix (`/data/data/com.termux/files/usr/bin`),
so a binary installed with `pkg install cloudflared` is detected automatically — no manual
upload required.

A running tunnel is **restarted automatically** when its process dies, and it is restored
after a device reboot. Press **Stop Tunnel** to shut it down permanently.

> Note: the Cloudflare quick tunnel uses a random URL that changes on every restart, and
> `cloudflared` running as root cannot use a personal cloudflared account without a
> named tunnel.

---

## 📋 Process Monitor Tab

The **Processes** tab lists every process running on the device, read straight from `/proc`
without spawning `ps` or `top`, so the panel stays lightweight.

1. Open the **Processes** tab in the **Core** category.
2. Use the search field to filter by process name, command line or runtime. The search runs
   with a 350 ms debounce.
3. The sort buttons switch the ordering: **CPU**, **Memory**, **Uptime**, **PID** and **Name**.
4. The panel refreshes itself every 3 seconds and can be disabled through the **Auto** toggle.

Every row shows the PID, the process name with its command line, the **runtime** badge, the
process owner (for example `system`, `u0_a123` or `root`) with the executable path, the CPU
share, the memory usage, the uptime and the thread count.

The runtime badge reports the interpreter behind a process: **Python**, **Node.js**, **Java**,
**PHP**, **Ruby**, **Perl**, **Lua**, **.NET**, **Shell**, **Go**, **Termux**, **OpenSSH**, and
**Kernel** for kernel threads.

The **Stop** button on a process row opens a confirmation dialog. **Stop Process** terminates
the process gracefully with `SIGTERM` and only forces it with `SIGKILL` when it ignores that
signal, while **Force Kill** sends `SIGKILL` right away. The daemon refuses to terminate
PID 1, itself and its own parent process.

---

## ⚙️ Configuration Customization (Optional)

You can override default password, ports, or paths by setting **Environment Variables** before launching the service.

Example adding a custom password inside `/data/adb/modules/termux_monitor/service.sh`:

```bash
export PORT=80
export TMON_PASSWORD="YourSuperSecurePassword123"
export TMON_SU_BIN="ksu"  # if using KernelSU root manager
$MODDIR/webui > /dev/null 2>&1 &
```

---

## ❓ Troubleshooting & FAQ

- **WebUI cannot be opened/timed out:**
  Verify if the process is running using a root terminal:
  ```bash
  su -c "pgrep -f webui"
  ```
- **Forgot Password:**
  Modify `TMON_PASSWORD` value in your startup script or target environment configuration, then restart the service.
