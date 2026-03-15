# RetroSync on Batocera — Installation and Startup Guide

Batocera uses a read-only root filesystem. Everything in this guide is placed under `/userdata`, which is the only partition that persists across reboots and Batocera updates.

---

## Prerequisites

### Enable SSH on the Batocera machine

From the Batocera main menu:
**Main Menu → Network Settings → Enable SSH** (toggle on)

Default SSH credentials:
- Username: `root`
- Password: `linux`

You will use SSH to copy files to the Batocera machine and to edit startup scripts.

### Download the correct RetroSync binary

| Machine | Binary to use |
|---|---|
| Batocera PC (x86_64) | `retrosync-linux-amd64` |
| Raspberry Pi 5 (ARM 64-bit) | `retrosync-linux-arm64` |

Download the appropriate binary from the RetroSync releases page.

---

## Installation

### 1. Create the RetroSync directory

SSH into the Batocera machine and create a directory to hold the RetroSync binary and config:

```bash
mkdir -p /userdata/system/retrosync
```

### 2. Copy the binary

From your PC (replace the filename and IP address as appropriate):

```bash
# PC binary
scp retrosync-linux-amd64 root@<batocera-ip>:/userdata/system/retrosync/retrosync

# Raspberry Pi binary
scp retrosync-linux-arm64 root@<batocera-ip>:/userdata/system/retrosync/retrosync
```

Then SSH in and make it executable:

```bash
chmod +x /userdata/system/retrosync/retrosync
```

### 3. Create a config file

Copy the example Batocera config as a starting point:

```bash
scp retrosyncBatocera.toml root@<batocera-ip>:/userdata/system/retrosync/retrosync.toml
```

Or create `/userdata/system/retrosync/retrosync.toml` directly on the Batocera machine and edit it. A minimal client config looks like this:

```toml
[node]
port           = 9877
discovery_port = 9876
role           = "client"
# server_addr  = "192.168.1.100:9877"  # set this if auto-discovery doesn't work

[[sync]]
name  = "snes-saves"
paths = [
    "/userdata/saves/snes/[*.srm;*.state;*.png]",
]
```

Uncomment and add `[[sync]]` blocks for each system you want to sync. Common Batocera save paths:

| System | Path |
|---|---|
| SNES | `/userdata/saves/snes/` |
| GBA | `/userdata/saves/gba/` |
| N64 | `/userdata/saves/n64/` |
| NES | `/userdata/saves/nes/` |
| Mega Drive | `/userdata/saves/megadrive/` |
| PS1 | `/userdata/saves/psx/` |

Batocera stores save states alongside save files in the same directory, so including `*.state` and `*.png` (thumbnail previews of states) in the patterns will sync those too.

---

## Startup at Boot

Batocera runs `/userdata/system/custom.sh` automatically on every boot if it exists and is executable. Add a RetroSync entry to this file to start it at startup.

### Create or edit custom.sh

SSH into the Batocera machine and open the file:

```bash
nano /userdata/system/custom.sh
```

Add the following (create the file from scratch if it doesn't exist):

```bash
#!/bin/bash

# Wait for network to be available (up to 15 seconds)
for i in $(seq 1 15); do
    ip route | grep -q default && break
    sleep 1
done

# Start RetroSync in the background
/userdata/system/retrosync/retrosync \
    -config /userdata/system/retrosync/retrosync.toml \
    >> /userdata/system/retrosync/retrosync.log 2>&1 &
```

Make the file executable:

```bash
chmod +x /userdata/system/custom.sh
```

**What the network wait does:** `custom.sh` runs early in boot before the network is necessarily up. The loop checks for a default route once per second for up to 15 seconds, then continues regardless. If RetroSync starts before the network is ready it will simply wait for server discovery on the next 30-second sync tick, so this is a precaution rather than a strict requirement.

**Log file:** RetroSync output is appended to `/userdata/system/retrosync/retrosync.log`. This file persists across reboots and is useful for troubleshooting.

---

## Verifying It Works

After rebooting the Batocera machine, from another machine on the same network open a browser and go to:

```
http://<batocera-ip>:9877/ui
```

The RetroSync web UI should show the node as connected and display its sync groups.

To check the log directly on the Batocera machine:

```bash
tail -f /userdata/system/retrosync/retrosync.log
```

---

## Updating RetroSync

To update to a new version of the RetroSync binary:

```bash
# Stop the running instance
pkill retrosync

# Copy the new binary
scp retrosync-linux-amd64 root@<batocera-ip>:/userdata/system/retrosync/retrosync
ssh root@<batocera-ip> "chmod +x /userdata/system/retrosync/retrosync"

# Restart it (or just reboot)
ssh root@<batocera-ip> "/userdata/system/retrosync/retrosync -config /userdata/system/retrosync/retrosync.toml >> /userdata/system/retrosync/retrosync.log 2>&1 &"
```

The config file is not affected by a binary update.
