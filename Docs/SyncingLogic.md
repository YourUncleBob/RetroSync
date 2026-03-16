# RetroSync — Syncing Logic

## Overview

RetroSync keeps save files consistent across multiple machines by periodically
comparing file indexes and transferring whichever copy of each file is newest.
The comparison is driven by two pieces of metadata: an **MD5 hash** (to detect
whether files differ at all) and a **modification time** (to decide which copy
wins when they do differ).

---

## Node Roles

A node operates in one of three roles, set via `role` in the config file.

| Role | Behaviour |
|---|---|
| `server` | Authoritative source of truth. Accepts uploads from clients; never initiates transfers. |
| `client` | Connects to a server (via UDP discovery or a configured `server_addr`). Runs a full bidirectional sync every 30 seconds. |
| `""` (legacy P2P) | No server. Every node discovers all peers via UDP and syncs with each of them. |

---

## File Indexing

Before any sync can happen, each node builds an in-memory **file index** — a
map of virtual paths to file metadata.

- **Virtual path** format: `<group-name>/<filename>` (e.g. `snes/zelda.srm`)
- **Metadata per file** (`FileInfo`):
  - `Hash` — MD5 of the file contents
  - `ModTime` — filesystem modification timestamp
  - `Size` — byte count
  - `LocalPath` — absolute OS path (never sent to peers)

The index is built at startup from all configured sync group directories and
kept up to date at runtime via **fsnotify**. When a file is written or created,
a 500 ms debounce fires, recomputes the hash and mod time for that file, and
updates the in-memory index under a mutex. `LocalPath` is excluded from JSON
serialisation so peers only see the virtual metadata.

---

## Peer Discovery

### Legacy P2P mode
Each node broadcasts a UDP beacon every 5 seconds on the configured discovery
port. All nodes on the LAN hear each other's beacons and add one another to
their peer list. Peers that have not sent a beacon within 15 seconds (3×
interval) are pruned.

### Server / Client mode
The server broadcasts its own beacon. Clients listen for it and record the
server's address and port on first contact. Clients also register themselves
with the server via HTTP identity headers (`X-RetroSync-ID`,
`X-RetroSync-Name`, `X-RetroSync-Port`) on every request, so the server can
maintain a live client list without requiring symmetric UDP.

---

## Sync Decision Logic

### Per-file rule (both modes)

For every file seen in the remote index:

1. **Same hash** → skip. The files are identical; no transfer needed.
2. **Different hash, remote mod time ≤ local mod time** → skip. The local copy
   is the same age or newer; it wins.
3. **Different hash, remote mod time > local mod time** → transfer. The remote
   copy is newer.

This means **modification time is the tiebreaker** whenever content differs.
The hash check comes first so that files with identical content are never
re-transferred even if their timestamps differ.

### Client/Server bidirectional sync (`syncWithServer`)

Runs immediately on startup and then every 30 seconds.

1. **PULL pass** — iterate the server's index. Download any file where the
   server copy is newer than (or absent from) the local index.
2. **Re-index** — after each successful pull, immediately re-index the
   downloaded file so the push pass has an accurate local snapshot.
3. **PUSH pass** — iterate the refreshed local index. Upload any file where the
   local copy is newer than (or absent from) the server's index.

### Legacy P2P sync (`syncWithPeer`)

Each peer relationship is pull-only from each side's perspective: every node
fetches files from each peer that are newer than its own copy. Because all
nodes run the same logic against all peers, convergence is eventual across the
whole mesh.

---

## Modification Time Replication

When a file is transferred, the **original modification timestamp is preserved
on the destination**. This is critical for correctness.

### How it works

**On upload (client → server):**
- `PushFile` (`transfer/client.go`) opens the local file, reads its mod time
  via `os.File.Stat()`, and sends it in the `X-RetroSync-ModTime` HTTP header
  (RFC3339Nano, UTC) alongside the file body.
- The server's PUT handler (`transfer/server.go`) writes the body to a temp
  file, renames it into place, and then calls `os.Chtimes` to restore the
  original mod time from the header.

**On download (server/peer → client):**
- The server serves files via `http.ServeFile`, which sets `Last-Modified` from
  the file's filesystem mod time.
- `FetchFile` (`transfer/client.go`) writes the file, then reads the
  `Last-Modified` response header and calls `os.Chtimes` to restore the
  original timestamp.

### Why this matters

Consider three nodes: a server, Client A, and Client B.

- Client A has an **older** version of a file, saved at `T=5`.
- Client B has a **newer** version of the same file, saved at `T=10`.
- Client A connects first.

**Without timestamp replication:**

1. Client A pushes its `T=5` file to the server. The OS stamps the server's
   copy with the wall-clock arrival time — say `T=20`.
2. Client B connects later with its `T=10` file.
3. During Client B's PULL pass, `remoteFile.ModTime (T=20) > localFile.ModTime
   (T=10)` → the server copy *looks* newer, so Client B downloads it,
   overwriting the actually-newer file with Client A's older version.

**With timestamp replication:**

1. Client A pushes its `T=5` file. The server restores the original mod time,
   so the server's copy is stamped `T=5`.
2. Client B connects with its `T=10` file.
3. During Client B's PULL pass, `remoteFile.ModTime (T=5) < localFile.ModTime
   (T=10)` → Client B's copy is newer; no download.
4. During Client B's PUSH pass, `localFile.ModTime (T=10) > remoteFile.ModTime
   (T=5)` → Client B uploads its newer file, correctly replacing the server's
   stale copy.

---

## Pausing

Individual sync groups can be paused via the API (`PATCH
/api/config/groups/{name}`) or the web UI. A paused group is skipped in both
the pull and push passes. Paused state is persisted to the TOML config file so
it survives restarts.

The server also exposes a global pause (`POST /api/pause-all`) that pauses
every group simultaneously.

---

## File Change Detection (fsnotify)

Outside of the periodic sync cycles, RetroSync reacts to live file changes:

- fsnotify watches every directory in every sync group.
- `Write` and `Create` events trigger a 500 ms debounce per file. After the
  debounce fires, the file is re-hashed and the in-memory index is updated.
- `Remove` and `Rename` events immediately remove the entry from the index.

This means a file saved locally will be picked up by the *next* 30-second sync
cycle (at most ~30 seconds later) without requiring a full directory rescan.

---

## On-Demand Sync Trigger

In addition to the 30-second periodic sync, a client node can be told to run a
normal bidirectional sync immediately via the HTTP API:

```
POST /api/sync
```

This runs exactly the same pull-then-push logic as the periodic cycle. It will
wait for any already in-progress sync to finish before starting, so it is safe
to call at any time. It returns `503 Service Unavailable` if the server has not
yet been discovered, and is not available on server or legacy P2P nodes (returns
`404`).

### Triggering with curl

```bash
curl -s -X POST http://localhost:9877/api/sync
```

A successful response:
```json
{"status":"ok"}
```

### Why this is useful for game launchers

Triggering a sync on game start and game exit is more precise than relying
solely on the periodic timer:

- **On game start** — pull the latest save from the server before the emulator
  launches, ensuring you never play from a stale local save.
- **On game exit** — push your updated save immediately after the emulator
  closes, before the machine sleeps or the player switches systems.

### Batocera

Batocera calls any executable scripts placed in `/userdata/system/scripts/`
automatically on game events. The script receives the event name as its first
argument (`gameStart`, `gameStop`, `systemStart`, `systemStop`).

```bash
#!/bin/bash
# /userdata/system/scripts/retrosync.sh

ACTION=$1

case "$ACTION" in
  gameStart|gameStop)
    curl -s -X POST http://localhost:9877/api/sync
    ;;
esac
```

Make the script executable:
```bash
chmod +x /userdata/system/scripts/retrosync.sh
```

### RetroBat

RetroBat (EmulationStation on Windows) supports pre- and post-launch scripts
configured per system or globally. Scripts are placed in:

```
%RETROBAT_ROOT%\system\scripts\
```

EmulationStation calls them with similar arguments (`game-start`, `game-end`).
A batch script example:

```bat
@echo off
REM %RETROBAT_ROOT%\system\scripts\retrosync.bat

set ACTION=%1

if "%ACTION%"=="game-start" goto sync
if "%ACTION%"=="game-end" goto sync
goto end

:sync
curl -s -X POST http://localhost:9877/api/sync

:end
```

Both platforms pass additional arguments (system name, ROM path) that can be
used to target a specific sync group if needed.
