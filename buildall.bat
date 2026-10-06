@echo off
rem Builds RetroSync for Windows PC, Batocera PC (x86_64) and Batocera Raspberry Pi 5 (arm64).
rem Outputs go to dist\ and the Windows build is copied to retrosync.exe in the project root.
setlocal

cd /d "%~dp0"

set VERSION=dev
for /f %%i in ('git rev-list --count HEAD') do set VERSION=%%i
echo Building RetroSync version %VERSION%

if not exist dist mkdir dist
set CGO_ENABLED=0

echo   windows/amd64 - dist\retrosync-windows-amd64.exe
set GOOS=windows
set GOARCH=amd64
go build -ldflags "-X main.version=%VERSION%" -o dist\retrosync-windows-amd64.exe .
if errorlevel 1 goto fail

echo   linux/amd64   - dist\retrosync-linux-amd64
set GOOS=linux
set GOARCH=amd64
go build -ldflags "-X main.version=%VERSION%" -o dist\retrosync-linux-amd64 .
if errorlevel 1 goto fail

echo   linux/arm64   - dist\retrosync-linux-arm64
set GOOS=linux
set GOARCH=arm64
go build -ldflags "-X main.version=%VERSION%" -o dist\retrosync-linux-arm64 .
if errorlevel 1 goto fail

copy /Y dist\retrosync-windows-amd64.exe retrosync.exe >nul
if errorlevel 1 goto fail

echo Done. retrosync.exe copied to project root.
exit /b 0

:fail
echo Build failed.
exit /b 1
