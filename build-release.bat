@echo off
setlocal enabledelayedexpansion

echo ========================================================
echo  Termux Monitor Dual-Variant Release Builder
echo ========================================================

:: Extract version from module.prop
set "VERSION=v1.3.0"
for /f "tokens=2 delims==" %%I in ('findstr "version=" module.prop 2^>nul') do (
    set "VERSION=%%I"
)
echo [INFO] Detected Version: !VERSION!

if not exist ".tmp" mkdir .tmp

:: ========================================================
:: 1. Build Flavor 1: Full Modem (dev target)
:: ========================================================
echo.
echo [1/4] Building frontend assets...
cd frontend
where bun >nul 2>nul
if !ERRORLEVEL! equ 0 (
    call bun run build
) else (
    call npm run build
)
if !ERRORLEVEL! neq 0 (
    echo [ERROR] Frontend build failed!
    cd ..
    exit /b 1
)
cd ..

echo [2/4] Cross-compiling Full Android ARM64 binary...
set "GOOS=android"
set "GOARCH=arm64"
set "CGO_ENABLED=0"

if exist ".tmp\pkg-full" rmdir /s /q ".tmp\pkg-full"
mkdir ".tmp\pkg-full"

go build -ldflags="-s -w" -o .tmp\pkg-full\webui .
if !ERRORLEVEL! neq 0 (
    echo [ERROR] Go build failed for Full Modem!
    exit /b 1
)

echo [INFO] Packaging Termux-Monitor-Magisk-!VERSION!.zip...
copy /y customize.sh .tmp\pkg-full\ >nul
copy /y module.prop .tmp\pkg-full\ >nul
copy /y service.sh .tmp\pkg-full\ >nul
copy /y system.prop .tmp\pkg-full\ >nul
copy /y update.json .tmp\pkg-full\ >nul
copy /y env.example .tmp\pkg-full\ >nul
copy /y uninstall.sh .tmp\pkg-full\ >nul
xcopy /e /i /y bin .tmp\pkg-full\bin >nul
xcopy /e /i /y META-INF .tmp\pkg-full\META-INF >nul

:: The webui-* files in bin\ are previous build outputs, not module payload.
if exist ".tmp\pkg-full\bin\webui-android-arm64" del /f /q ".tmp\pkg-full\bin\webui-android-arm64"
if exist ".tmp\pkg-full\bin\webui-android-arm64-nomodem" del /f /q ".tmp\pkg-full\bin\webui-android-arm64-nomodem"

if exist "Termux-Monitor-Magisk-!VERSION!.zip" del /f /q "Termux-Monitor-Magisk-!VERSION!.zip"
tar -a -c -f "Termux-Monitor-Magisk-!VERSION!.zip" -C .tmp\pkg-full META-INF customize.sh module.prop service.sh system.prop update.json env.example uninstall.sh bin webui
if !ERRORLEVEL! neq 0 (
    powershell -NoProfile -Command "Compress-Archive -Path '.tmp\pkg-full\*' -DestinationPath 'Termux-Monitor-Magisk-!VERSION!.zip' -Force"
)
echo [SUCCESS] Full Modem Zip created: Termux-Monitor-Magisk-!VERSION!.zip

:: ========================================================
:: 2. Build Flavor 2: Clean No-Modem (public target)
:: ========================================================
:: The vendor modem seam has been retired: the full and no-modem
:: flavors now build from identical frontend assets, so no per-flavor
:: frontend source rewrite is required. Only the Go build tag differs.
echo.
echo [3/4] Reusing frontend assets for No-Modem build...

echo [4/4] Cross-compiling No-Modem Android ARM64 binary...
if exist ".tmp\pkg-nomodem" rmdir /s /q ".tmp\pkg-nomodem"
mkdir ".tmp\pkg-nomodem"

go build -tags nomodem -ldflags="-s -w" -o .tmp\pkg-nomodem\webui .
if !ERRORLEVEL! neq 0 (
    echo [ERROR] Go build failed for No-Modem!
    exit /b 1
)

:: Run security audit if python available
python --version >nul 2>&1
if !ERRORLEVEL! equ 0 (
    echo [INFO] Running zero-leak forensic audit on No-Modem binary...
    python scripts\audit_nomodem.py .tmp\pkg-nomodem\webui
    if !ERRORLEVEL! neq 0 (
        echo [SECURITY WARNING] Audit reported signatures in No-Modem binary!
    )
)

echo [INFO] Packaging Termux-Monitor-Magisk-!VERSION!-nomodem.zip...
copy /y customize.sh .tmp\pkg-nomodem\ >nul
copy /y module.prop .tmp\pkg-nomodem\ >nul
copy /y service.sh .tmp\pkg-nomodem\ >nul
copy /y system.prop .tmp\pkg-nomodem\ >nul
copy /y update.json .tmp\pkg-nomodem\ >nul
copy /y env.example .tmp\pkg-nomodem\ >nul
copy /y uninstall.sh .tmp\pkg-nomodem\ >nul
xcopy /e /i /y bin .tmp\pkg-nomodem\bin >nul
xcopy /e /i /y META-INF .tmp\pkg-nomodem\META-INF >nul

:: The webui-* files in bin\ are previous build outputs, not module payload.
if exist ".tmp\pkg-nomodem\bin\webui-android-arm64" del /f /q ".tmp\pkg-nomodem\bin\webui-android-arm64"
if exist ".tmp\pkg-nomodem\bin\webui-android-arm64-nomodem" del /f /q ".tmp\pkg-nomodem\bin\webui-android-arm64-nomodem"

if exist "Termux-Monitor-Magisk-!VERSION!-nomodem.zip" del /f /q "Termux-Monitor-Magisk-!VERSION!-nomodem.zip"
tar -a -c -f "Termux-Monitor-Magisk-!VERSION!-nomodem.zip" -C .tmp\pkg-nomodem META-INF customize.sh module.prop service.sh system.prop update.json env.example uninstall.sh bin webui
if !ERRORLEVEL! neq 0 (
    powershell -NoProfile -Command "Compress-Archive -Path '.tmp\pkg-nomodem\*' -DestinationPath 'Termux-Monitor-Magisk-!VERSION!-nomodem.zip' -Force"
)
echo [SUCCESS] Clean No-Modem Zip created: Termux-Monitor-Magisk-!VERSION!-nomodem.zip

:: Clean up staging dirs
rmdir /s /q ".tmp\pkg-full" >nul 2>&1
rmdir /s /q ".tmp\pkg-nomodem" >nul 2>&1

echo.
echo ========================================================
echo  All 2 Release Variants Successfully Built!
echo   1. Termux-Monitor-Magisk-!VERSION!.zip
echo   2. Termux-Monitor-Magisk-!VERSION!-nomodem.zip
echo ========================================================
