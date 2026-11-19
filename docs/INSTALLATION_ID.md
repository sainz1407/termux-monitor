# Panduan Instalasi Termux Monitor (Bahasa Indonesia)

Dokumen ini berisi panduan langkah demi langkah untuk menginstal dan menjalankan **Termux Monitor** di perangkat Android yang telah di-root (menggunakan Magisk, KernelSU, atau APatch).

---

## 📋 Persyaratan Sistem

1. Perangkat Android ter-root (Android 8.0+ direkomendasikan).
2. Salah satu Root Manager berikut:
   - **Magisk** v20.4+
   - **KernelSU** v0.6.0+
   - **APatch** v10.7+
3. Ruang penyimpanan minimal ~25MB.

---

## 🚀 Cara Instalasi

### Metode 1: Instalasi via Zip Modul (Direkomendasikan)

1. Unduh rilis file zip modul terbaru `Termux-Monitor-Magisk-vX.Y.Z.zip` dari halaman [Releases](https://github.com/sainz1407/termux-monitor/releases).
2. Buka aplikasi Root Manager Anda (**Magisk App**, **KernelSU App**, atau **APatch App**).
3. Masuk ke menu **Modules** / **Modul**.
4. Pilih **Install from storage** / **Pasang dari penyimpanan** dan pilih file zip yang telah diunduh.
5. Tunggu proses flashing selesai, lalu lakukan **Reboot / Muat Ulang Perangkat**.

---

### Metode 2: Instalasi Manual via Terminal / Shell Root

Jika Anda ingin memasang atau menguji binary secara manual tanpa membuat paket modul zip:

1. Salin binary `webui` ke direktori modul:
   ```bash
   su
   mkdir -p /data/adb/modules/termux_monitor
   cp /sdcard/Download/webui /data/adb/modules/termux_monitor/
   chmod 755 /data/adb/modules/termux_monitor/webui
   ```

2. Buat file `module.prop` dasar:
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

3. Buat script startup `service.sh`:
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

4. Jalankan service secara langsung atau reboot perangkat:
   ```bash
   /data/adb/modules/termux_monitor/service.sh &
   ```

---

## 🌐 Cara Mengakses WebUI

1. Pastikan perangkat terhubung ke Wi-Fi atau gunakan IP lokal perangkat.
2. Buka browser (Chrome, Kiwi, Firefox, dll) di perangkat Android Anda atau perangkat lain di jaringan lokal yang sama.
3. Masuk ke URL:
   ```text
   http://localhost:80
   ```
   *Atau dari perangkat lain di Wi-Fi yang sama:*
   ```text
   http://<IP_ANDROID_ANDA>:80
   ```
4. Masukkan password default saat login:
   ```text
   tmon
   ```

### 🎥 Catatan Mirror Layar (Scrcpy)

Mirror layar memakai **WebCodecs H.264**, yang hanya tersedia di *secure context* (HTTPS atau `localhost`).
Saat WebUI diakses lewat `http://<IP_ANDROID_ANDA>:80` dari perangkat lain, browser tidak menyediakan
decoder H.264, sehingga aplikasi otomatis beralih ke mode **MJPEG (screencap ~3 FPS)** — layar tetap
tampil dan sentuhan/remote control tetap berfungsi, hanya frame rate-nya rendah.

Untuk kecepatan penuh (H.264 60 FPS), gunakan salah satu opsi berikut:
- Akses WebUI melalui `http://localhost:80` dari browser di perangkat Android itu sendiri.
- Akses WebUI melalui HTTPS dan pasang sertifikat modul (`/data/adb/modules/termux_monitor/tls/cert.pem`)
  sebagai CA tepercaya di perangkat klien.

---

## 🌐 Tunnel Akses Jarak Jauh (Remote Access)

Tab **Tunnel** bisa membuka WebUI dari luar jaringan tanpa port forwarding.

1. Pilih engine: **Cloudflare** (quick tunnel gratis), **Tailscale**, **ZeroTier**, **Ngrok**, atau **Pinggy**.
2. Untuk Cloudflare, biarkan token kosong agar memakai *quick tunnel* gratis, atau isi token
   `cloudflared tunnel run --token ...` untuk named tunnel.
3. Klik **Start Tunnel**. Public URL akan muncul setelah ±10 detik dan juga tersedia di
   panel log.

Engine binary dicari di direktori data modul, di `PATH`, di `/data/adb/modules/*/bin`,
dan di prefix Termux (`/data/data/com.termux/files/usr/bin`). Jadi binary yang diinstal
lewat `pkg install cloudflared` langsung terdeteksi — tidak perlu upload manual.

Tunnel yang sedang berjalan otomatis **dijalankan ulang** kalau prosesnya mati, dan
otomatis aktif kembali setelah perangkat reboot. Untuk berhenti permanen, tekan
**Stop Tunnel**.

> Catatan: quick tunnel Cloudflare memakai URL acak yang berubah setiap restart, dan
> `cloudflared` yang berjalan sebagai root tidak bisa memakai akun cloudflared pribadi
> tanpa named tunnel.

---

## 📋 Tab Process Monitor

Tab **Processes** menampilkan seluruh proses yang sedang berjalan di perangkat, dibaca langsung
dari `/proc` tanpa menjalankan `ps` atau `top`, sehingga bobotnya tetap ringan.

1. Buka tab **Processes** pada kategori **Core**.
2. Gunakan kolom pencarian untuk memfilter berdasarkan nama proses, command line, atau runtime.
   Pencarian berjalan dengan *debounce* 350 ms.
3. Tombol sortir mengganti urutan: **CPU**, **Memory**, **Uptime**, **PID**, dan **Name**.
4. Panel ini memperbarui diri sendiri setiap 3 detik, dan dapat dimatikan lewat tombol **Auto**.

Setiap baris menampilkan PID, nama proses beserta command line, badge **runtime**, pemilik
proses (contoh `system`, `u0_a123`, `root`) beserta path executable, persentase CPU,
pemakaian memori, waktu berjalan, dan jumlah thread.

Badge runtime menandai interpreter di balik sebuah proses: **Python**, **Node.js**, **Java**,
**PHP**, **Ruby**, **Perl**, **Lua**, **.NET**, **Shell**, **Go**, **Termux**, **OpenSSH**,
serta **Kernel** untuk kernel thread.

Tombol **Stop** pada baris proses membuka dialog konfirmasi. **Stop Process** menghentikan
proses secara halus dengan sinyal `SIGTERM`, lalu dipaksa dengan `SIGKILL` hanya bila proses
mengabaikan sinyal tersebut. **Force Kill** langsung mengirim `SIGKILL` tanpa menunggu.
Daemon menolak untuk menghentikan PID 1, dirinya sendiri, dan proses induknya.

---

## ⚙️ Kustomisasi Konfigurasi (Optional)

Anda dapat merubah password, port, atau path default dengan menyetel **Environment Variables** sebelum layanan berjalan. 

Contoh menambahkan password kustom di `/data/adb/modules/termux_monitor/service.sh`:

```bash
export PORT=80
export TMON_PASSWORD="PasswordSangatAman123"
export TMON_SU_BIN="ksu"  # jika menggunakan KernelSU
$MODDIR/webui > /dev/null 2>&1 &
```

---

## ❓ Troubleshooting & Pertanyaan Umum

- **WebUI tidak bisa dibuka:**
  Cek apakah proses berjalan menggunakan terminal root:
  ```bash
  su -c "pgrep -f webui"
  ```
- **Lupa Password:**
  Ubah variabel `TMON_PASSWORD` di `service.sh` atau restart service setelah menyetel env variable baru.
