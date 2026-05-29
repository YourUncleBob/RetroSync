# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build Commands

```bash
# Build for current platform (bash / Git Bash)
VERSION=$(git rev-list --count HEAD)
go build -ldflags "-X main.version=$VERSION" -o retrosync .

# Cross-platform builds — use the batch script on Windows cmd/PowerShell
buildall.bat
# Produces: dist/retrosync-windows-amd64.exe, dist/retrosync-linux-amd64, dist/retrosync-linux-arm64

# Run tests
go test ./internal/...

# Run a single test package
go test ./internal/node/
```

## Running

```bash
# With config file (preferred)
retrosync -config retrosync.toml

# Legacy mode (no config, uses default ports)
retrosync -dir <sync_directory>

# Flags: -port 9877 (HTTP), -discovery 9876 (UDP)
```

Web UI is available at `http://localhost:<port>/ui` once running.

## Architecture

RetroSync syncs retro gaming save files across systems with different folder structures. A node runs in one of three modes determined by `role` in the config:

- **`server`** — Authoritative source of truth. Accepts pushed files from clients, does not pull.
- **`client`** — Discovers server via UDP or configured `server_addr`. Syncs every 30s bidirectionally (pull newer from server, push newer to server).
- **`""` (legacy P2P)** — All nodes discover each other; each syncs with all peers.

### Package Responsibilities

| Package | Role |
|---|---|
| `internal/config` | TOML parsing, path spec parsing (`"dir/[*.srm;*.png]"` format), default config generation |
| `internal/node` | Central orchestrator — coordinates all subsystems, owns `fileIdx` (virtual path → FileInfo), runs the 30s sync loops |
| `internal/index` | Builds and maintains file inventory with MD5 hashes; defines virtual paths as `"group-name/filename"` (flat) or `"group-name/subdir/filename"` (recursive) |
| `internal/discovery` | UDP broadcast/listen for LAN peer discovery; beacons every 5s |
| `internal/transfer` | HTTP server (endpoints below) and HTTP client for index fetching, file download/upload |

### HTTP Endpoints (transfer/server.go)

- `GET /index` — JSON file index
- `GET /files/{group}/{filename}` — Download file
- `PUT /files/{group}/{filename}` — Upload file (clients push here); restores original mod time from `X-RetroSync-ModTime` header
- `GET /ui` — Web dashboard (embedded `ui.html`)
- `GET /api/status` — Node name, version, uptime, file count, role, connected peers
- `GET /api/config` — Sync group list with file counts
- `GET /api/server/config` — (client only) fetch server's sync group config
- `POST /api/config/groups` — Add sync group at runtime; body: `{"name":"…","paths":[…]}`
- `DELETE /api/config/groups/{name}` — Remove group
- `PATCH /api/config/groups/{name}` — Pause/resume group; body: `{"paused": true}`
- `POST /api/pause-all` — Pause/resume all groups; body: `{"paused": true}`
- `POST /api/force-sync` — (client only) authoritative pull from server; body: `{"group":""}` for all groups
- `POST /api/sync` — (client only) trigger normal bidirectional sync with cooldown throttling
- `GET /api/log` — Event buffer; supports `?after=<index>` for polling

### Sync Decision Logic

Files are compared by MD5 hash and modification time. A file is downloaded/uploaded only when the remote copy is newer (by mod time, after hash mismatch confirms they differ). Client stamps all requests with `X-RetroSync-*` headers so the server can track connected clients.

### File Change Detection

fsnotify watches all sync group directories. On write/create events, a 500ms debounce fires, re-indexes the file (recomputes MD5 + mod time), and updates `fileIdx` under an RWMutex. The next sync cycle picks up the change.

### Runtime Config Mutations

`AddGroup()`, `RemoveGroup()`, `PauseGroup()` on the `Node` struct mutate the running state, update the watcher, re-index as needed, and persist to the config TOML file if `cfgPath` is set.

### General Rules
- Do not commit and push changes automatically after testing. I would like to review the changes myself before commiting
- Do not use HTML style comments in the code. Just use simple comments
- After making a change, update verify that the README.md and all .md files in the Docs folder are accurate and update them to be accurate if they are not