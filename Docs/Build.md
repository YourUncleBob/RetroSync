# Building RetroSync

RetroSync is a single Go program with no C dependencies, so every target can be cross-compiled from one machine. These instructions assume you are building on a Windows PC, but the Git Bash commands work the same on Linux or macOS.

| Target | Go `GOOS/GOARCH` | Build output | Name to deploy as |
|---|---|---|---|
| Windows PC | `windows/amd64` | `dist\retrosync-windows-amd64.exe` | `retrosync.exe` |
| Batocera PC (x86_64) | `linux/amd64` | `dist/retrosync-linux-amd64` | `/userdata/system/retrosync/retrosync` |
| Batocera Raspberry Pi 5 | `linux/arm64` | `dist/retrosync-linux-arm64` | `/userdata/system/retrosync/retrosync` |

Built binaries are not checked in to git. `dist/`, `*.exe` and the Linux binary names are all listed in `.gitignore`.

---

## Prerequisites

1. **Go 1.21 or newer.** Download it from https://go.dev/dl/ (on Windows, the `.msi` installer adds `go` to your `PATH`). Or install it with winget:

   ```
   winget install GoLang.Go
   ```

   Open a new terminal afterwards and check that it works:

   ```
   go version
   ```

2. **Git.** The build uses the git commit count as the version number, so build from a git clone, not a downloaded zip. Without git, the version shows as `dev`.

3. **Dependencies.** Go downloads these on the first build. To fetch them ahead of time:

   ```
   go mod download
   ```

---

## Version number

The version shown in the web UI and the log is set at build time:

```
-ldflags "-X main.version=<number>"
```

`<number>` is the output of `git rev-list --count HEAD`. Without this flag, the binary reports version `dev`.

---

## Option 1: buildall.bat (Windows cmd or PowerShell)

From the project root:

```
buildall.bat
```

In PowerShell, run it as `.\buildall.bat`.

The script:

1. Reads the commit count for the version number.
2. Builds all three targets into `dist\` with `CGO_ENABLED=0`:
   - `dist\retrosync-windows-amd64.exe`
   - `dist\retrosync-linux-amd64`
   - `dist\retrosync-linux-arm64`
3. Copies `dist\retrosync-windows-amd64.exe` to `retrosync.exe` in the project root.

It stops at the first build that fails.

---

## Option 2: Manual builds

All commands are run from the project root. `CGO_ENABLED=0` makes the Linux binaries fully static, so they run on Batocera without depending on its system libraries.

### PowerShell

```powershell
$env:VERSION = git rev-list --count HEAD
$env:CGO_ENABLED = "0"
New-Item -ItemType Directory -Force dist | Out-Null

# Windows PC
$env:GOOS = "windows"; $env:GOARCH = "amd64"
go build -ldflags "-X main.version=$env:VERSION" -o dist/retrosync-windows-amd64.exe .
Copy-Item dist/retrosync-windows-amd64.exe retrosync.exe -Force

# Batocera PC (x86_64)
$env:GOOS = "linux"; $env:GOARCH = "amd64"
go build -ldflags "-X main.version=$env:VERSION" -o dist/retrosync-linux-amd64 .

# Batocera Raspberry Pi 5
$env:GOOS = "linux"; $env:GOARCH = "arm64"
go build -ldflags "-X main.version=$env:VERSION" -o dist/retrosync-linux-arm64 .

# Clear the cross-compile settings so later go commands target this PC again
Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED
```

### cmd

```bat
for /f %i in ('git rev-list --count HEAD') do set VERSION=%i
set CGO_ENABLED=0
if not exist dist mkdir dist

rem Windows PC
set GOOS=windows& set GOARCH=amd64
go build -ldflags "-X main.version=%VERSION%" -o dist\retrosync-windows-amd64.exe .
copy /Y dist\retrosync-windows-amd64.exe retrosync.exe

rem Batocera PC (x86_64)
set GOOS=linux& set GOARCH=amd64
go build -ldflags "-X main.version=%VERSION%" -o dist\retrosync-linux-amd64 .

rem Batocera Raspberry Pi 5
set GOOS=linux& set GOARCH=arm64
go build -ldflags "-X main.version=%VERSION%" -o dist\retrosync-linux-arm64 .
```

Typed at the prompt, `for` uses `%i`. Inside a `.bat` file it must be `%%i`.

### Git Bash (or Linux/macOS)

```bash
VERSION=$(git rev-list --count HEAD)
mkdir -p dist

# Windows PC
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "-X main.version=$VERSION" -o dist/retrosync-windows-amd64.exe .
cp dist/retrosync-windows-amd64.exe retrosync.exe

# Batocera PC (x86_64)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-X main.version=$VERSION" -o dist/retrosync-linux-amd64 .

# Batocera Raspberry Pi 5
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "-X main.version=$VERSION" -o dist/retrosync-linux-arm64 .
```

---

## Running the tests

```
go test ./internal/...
```

---

## Deploying the builds

- **Windows PC:** copy `retrosync.exe` to its install location, such as `C:\ProgramData\RetroSync\`. See [WindowsServiceSetup.md](WindowsServiceSetup.md). If RetroSync is already installed as a service, stop it before you replace the exe.
- **Batocera PC:** copy `dist/retrosync-linux-amd64` to the Batocera machine as `/userdata/system/retrosync/retrosync`, then `chmod +x` it. See [BatoceraSetup.md](BatoceraSetup.md).
- **Batocera Raspberry Pi 5:** same steps as the Batocera PC, but use `dist/retrosync-linux-arm64`.

For example, from Git Bash:

```bash
scp dist/retrosync-linux-arm64 root@<batocera-ip>:/userdata/system/retrosync/retrosync
ssh root@<batocera-ip> chmod +x /userdata/system/retrosync/retrosync
```

To create a starter config file, run the built binary with `-createconfig`:

```
retrosync.exe -createconfig retrosync.toml
```

---

## Troubleshooting

- **`go` is not recognized:** Go is not installed, or the terminal was opened before Go was installed. Open a new terminal.
- **Version shows `dev`:** the `-ldflags` value was empty. Check that `git rev-list --count HEAD` works in the project folder.
- **`exec format error` on Batocera:** the binary was built for the wrong architecture. The Pi 5 needs `arm64` and a Batocera PC needs `amd64`.
- **`Permission denied` on Batocera:** run `chmod +x` on the binary.
- **Later `go build` or `go run` produces a Linux binary on Windows:** `GOOS`/`GOARCH` are still set in that terminal. Clear them as shown in the PowerShell section, or open a new terminal.
