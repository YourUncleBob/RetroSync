# RetroSync
A golang file sync system that is intended to be used to sync save files between multiple Batocera or Retrobat systems.
Supports Batocera and Retrobat using different folders for some of the save files (where in Batocera the save files with different extensions are all in a single folder and in Retrobat they are in separate folders)
## Status
RetroSync is currently working and has been tested with Windows PC Retrobat systems and PC Batocera. I have built it for Batocera Raspberry PI, but have not tested it.

## The problem:
My home setup is a couple of Windows PC's running retrobat, a couple of Batocera RaspBerry PI 5's, and a Batocera PC. I want to sync save files between all of these systems.
The save file folders for Batocera and Retrobat are not the same. Batocera places all of its save files in a single folder per system and Retrobat splits its save files between two folders.
I couldn't find a great way to get Syncthing to sync the files the way I wanted it to.
I had no luck getting Retrobat to put all save files in a single folder
I had no luck getting Batocera to split the save files into the same structure as Retrobat (and frankly didn't want to have to change configurations for the Batocera systems even if I could get it working)

In Batocera, the save files look like:
```
/userdata/saves/snes/a.srm
/userdata/saves/snes/a.state
/userdata/saves/snes/a.state.png
/userdata/saves/snes/a.state1
/userdata/saves/snes/a.state1.png
...
```

In retrobat, they look like:
```
C:/RetroBat/saves/snes/a.srm
C:/RetroBat/saves/snes/libretro.snes9x/a.state
C:/RetroBat/saves/snes/libretro.snes9x/a.state.png
C:/RetroBat/saves/snes/libretro.snes9x/a.state1
C:/RetroBat/saves/snes/libretro.snes9x/a.state1.png
...
```

In addition, I have one PC that's always on. I wanted it to be an authoritative Server (it also has Retrobat on it and uses the save files) and not use a peer-to-peer system.

It was easier to throw together RetroSync than figure out a way to get the current systems working.


## Example config file

### Retrobat (server)
```toml
# RetroSync configuration file - Retrobat on PC (server node)

[node]
port           = 9877
discovery_port = 9876
role           = "server"

# Each [[sync]] block defines a named group of files to sync.
# Files are shared with peers as "<group-name>/<filename>".
# Peers map the same group name to their own local paths.
#
# Path format:  "path/to/dir/"               — sync all files in dir
#               "path/to/dir/[*.srm]"         — sync only .srm files
#               "path/to/dir/[*.state;*.png]" — sync .state and .png files

[[sync]]
name  = "snes-saves"
# When copying to the server, copy all .srm files from the snes folder and all .state and .png
# files from the snes/libretro.snes9x folder
#
# When copying files down from the server, .srm files will be placed in the snes folder and
# .state and .png files in the snes/libretro.snes9x folder
paths = [
    "J:/RetroBat/saves/snes/[*.srm]",
    "J:/RetroBat/saves/snes/libretro.snes9x/[*.state;*.png]"
]
```
### Batocera (client)
```toml
# RetroSync configuration file — Batocera on any system (client node)

[node]
port           = 9877
discovery_port = 9876
role           = "client"
# server_addr  = "192.168.1.x"  # optional; omit to use UDP auto-discovery
# sync_interval = 30            # seconds between periodic background syncs (default 30)
# sync_cooldown = 120           # minimum seconds between triggered syncs (default 120)

[[sync]]
name  = "snes-saves"
# All .srm, .state and .png files reside in the snes folder
paths = [
    "/userdata/saves/snes/[*.srm;*.state;*.png]",
]
```

## Sync Groups

A sync group is a named collection of files that RetroSync tracks and synchronises across nodes. Every node that participates in syncing a set of files must define a sync group with the **same name**; RetroSync uses that shared name to match files between nodes even when the files live in completely different directories.

### How a sync group works

Each group is defined by one or more **path specs** — entries in the `paths` list of a `[[sync]]` block. A path spec points RetroSync at a directory and, optionally, a set of filename patterns:

| Path spec | What is synced |
|---|---|
| `"path/to/dir/"` | All files directly in `dir` |
| `"path/to/dir/[*.srm]"` | Only `.srm` files in `dir` |
| `"path/to/dir/[*.state;*.png]"` | Files matching either pattern |

Multiple path specs in the same group let you pull files **from different directories into a single logical group**. This is the key feature that handles the Batocera/Retrobat difference: a Retrobat node might map `snes-saves` to two directories (`saves/snes/` for `.srm` and `saves/snes/libretro.snes9x/` for state files), while a Batocera node maps the same group name to one directory where all those files sit together.

### Virtual paths

Internally, every file is identified by a **virtual path** of the form `group-name/filename` (e.g. `snes-saves/zelda.srm`). Virtual paths are what nodes compare and transfer — the local directory layout is irrelevant. When a file arrives from another node, RetroSync looks at the filename's extension and finds the first path spec in the local group whose pattern matches, then writes the file there.

### Sync rules

- Files are compared by **MD5 hash** and **modification time**. A file is only transferred when the remote copy has a different hash *and* a newer modification time — so identical files are never re-sent.
- Syncing is **not recursive**. Only files directly inside a specified directory are included; subdirectories are ignored unless listed as their own path spec.
- Groups can be **paused** individually or all at once, either from the web UI or the API. Paused groups are skipped during sync cycles.

### Defining groups in config

```toml
[[sync]]
name  = "snes-saves"
paths = [
    "C:/RetroBat/saves/snes/[*.srm]",
    "C:/RetroBat/saves/snes/libretro.snes9x/[*.state;*.png]"
]

[[sync]]
name  = "nes-saves"
paths = [
    "C:/RetroBat/saves/nes/[*.srm;*.state;*.png]"
]
```

Groups can also be added, removed, or paused at runtime through the web UI or the REST API without restarting RetroSync.

## Authoritative Server
Because of my setup at home, it made the most sense for me to use an authoritative server where all clients push changes up to the server and get new files down from the server. This will allow me to do some better conflict resolution (it doesn't exist in the current version) and do things like keep older versions of the save files to restore back to should a save file somehow be damaged.
RetroSync also supports legacy peer-to-peer mode (omit `role` from the config), where all nodes discover each other and sync bidirectionally. In general, this was intended to sync smallish files infrequently, so it doesn't attempt to be as failsafe as Syncthing.

A **Force Sync** command is available in the web UI and API (`POST /api/force-sync`). It performs an authoritative pull from the server for a group or all groups, downloading every server file unconditionally and deleting any local files not present on the server.

A **Triggered Sync** (`POST /api/sync`) runs the same normal bidirectional pull-then-push sync as the periodic cycle, but on demand. The first call fires immediately; further calls within the cooldown window (default 2 minutes, configurable via `sync_cooldown`) are suppressed. This is designed for use by game launcher scripts — triggering a sync when a game is selected and when it exits ensures saves are always up to date at the right moments without flooding RetroSync with requests during game browsing.

## Documentation
The Docs folder contains detailed documentation for setting up RetroSync on Windows and Batocera systems. 

## Building
I did all development in JetBrains GoLand on a Windows PC. I believe this can be built on any platform that supports golang, but I've only tried it from Windows.

The build embeds a version number (the git commit count) via `-ldflags`.

### On PC from CMD prompt
Use `buildall.bat` — it captures the commit count, builds all three targets, and copies the Windows binary to `retrosync.exe` in the project root:

    buildall.bat

Outputs: `dist\retrosync-windows-amd64.exe`, `dist\retrosync-linux-amd64`, `dist\retrosync-linux-arm64`

### From Git Bash prompt

    VERSION=$(git rev-list --count HEAD)

    PC
    GOOS=windows GOARCH=amd64 go build -ldflags "-X main.version=$VERSION" -o dist/retrosync-windows-amd64.exe .

    Batocera X86_64
    GOOS=linux GOARCH=amd64 go build -ldflags "-X main.version=$VERSION" -o dist/retrosync-linux-amd64 .

    Batocera Raspberry PI 5
    GOOS=linux GOARCH=arm64 go build -ldflags "-X main.version=$VERSION" -o dist/retrosync-linux-arm64 .
    
## Web Monitoring/Configuration
Once running, a web UI can be brought up at http://localhost:9877/ui. This shows the status of the system, what it's connected to and all of the current sync groups that are defined. It also allows for new sync groups to be created. The Node Info panel includes uptime and a running count of files synced since the node started.
<img alt="RetroSyncWeb" src="https://github.com/YourUncleBob/RetroSync/blob/main/images/RetroSyncWeb.png" />


## Next Steps
My current plan is:
* Test on more Batocera platforms (I have access to Batocera PC, Batocera Raspberry PI 5), I have only tested on Batocera PC
* Currently the syncing doesn't recursively go into folders. It only syncs files directly in specified folders. For my needs, this is all I need, but it may be worthwhile to add the ability to specify that a sync group should include recursion.
* Peer-to-peer support - Legacy P2P mode is implemented (omit `role` from config) but is largely untested. It works by having all nodes discover each other via UDP and sync bidirectionally.
* I've been looking into using a Google drive to have each system just sync directly to the Google drive. It looks relatively easy to implement, but the authorization looks to be a pain. I'd either need to jump through the google approval hoops to get this app approved, and then figure out how to distribute the app with those credentials embedded, or anyone who uses the Google sync feature would need to provide their own app credentials that RetroSync would load and use 

