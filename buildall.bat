@echo off
for /f %%i in ('git rev-list --count HEAD') do set VERSION=%%i
echo Building RetroSync version %VERSION%...
retrosync -service stop

if not exist dist mkdir dist
if exist dist\retrosync-linux-amd64 del dist\retrosync-linux-amd64
if exist dist\retrosync-linux-arm64 del dist\retrosync-linux-arm64
set LDFLAGS=-X main.version=%VERSION%


echo Building windows/amd64...
set GOOS=windows
set GOARCH=amd64
go build -ldflags "%LDFLAGS%" -o dist\retrosync-windows-amd64.exe .
if errorlevel 1 ( echo FAILED && exit /b 1 )
echo Built dist\retrosync-windows-amd64.exe

echo Building linux/amd64...
set GOOS=linux
set GOARCH=amd64
go build -ldflags "%LDFLAGS%" -o dist\retrosync-linux-amd64.exe .
if errorlevel 1 ( echo FAILED && exit /b 1 )
ren dist\retrosync-linux-amd64.exe retrosync-linux-amd64
echo Built dist\retrosync-linux-amd64

echo Building linux/arm64...
set GOOS=linux
set GOARCH=arm64
go build -ldflags "%LDFLAGS%" -o dist\retrosync-linux-arm64.exe .
if errorlevel 1 ( echo FAILED && exit /b 1 )
ren dist\retrosync-linux-arm64.exe retrosync-linux-arm64
echo Built dist\retrosync-linux-arm64

copy /Y dist\retrosync-windows-amd64.exe retrosync.exe > nul
if exist retrosync.exe echo Copied dist\retrosync-windows-amd64.exe to retrosync.exe
if not exist retrosync.exe echo Error copying dist\retrosync-windows-amd64.exe to retrosync.exe

copy /Y retrosync.exe C:\ProgramData\RetroSync\ > nul
if exist C:\ProgramData\RetroSync\retrosync.exe echo Copied retrosync.exe to C:/ProgramData/RetroSync
if not exist C:\ProgramData\RetroSync\retrosync.exe echo Error copying retrosync.exe to C:/ProgramData/RetroSync
echo Done

