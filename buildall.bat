@echo off
for /f %%i in ('git rev-list --count HEAD') do set VERSION=%%i
echo Building RetroSync version %VERSION%...

if not exist dist mkdir dist

set LDFLAGS=-X main.version=%VERSION%

echo Building windows/amd64...
set GOOS=windows
set GOARCH=amd64
go build -ldflags "%LDFLAGS%" -o dist\retrosync-windows-amd64.exe .
if errorlevel 1 ( echo FAILED && exit /b 1 )

echo Building linux/amd64...
set GOOS=linux
set GOARCH=amd64
go build -ldflags "%LDFLAGS%" -o dist\retrosync-linux-amd64.exe .
if errorlevel 1 ( echo FAILED && exit /b 1 )
ren dist\retrosync-linux-amd64.exe retrosync-linux-amd64

echo Building linux/arm64...
set GOOS=linux
set GOARCH=arm64
go build -ldflags "%LDFLAGS%" -o dist\retrosync-linux-arm64.exe .
if errorlevel 1 ( echo FAILED && exit /b 1 )
ren dist\retrosync-linux-arm64.exe retrosync-linux-arm64

echo Done. Outputs in dist\
