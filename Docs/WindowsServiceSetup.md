# RetroSync on Windows — Running as a Service

Running RetroSync as a Windows service means it starts automatically at boot, runs in the background without a console window, and restarts if the machine reboots — without anyone needing to be logged in.

---

## Prerequisites

- **RetroSync binary** — `retrosync-windows-amd64.exe` (rename to `retrosync.exe` for convenience)
- **Administrator access** — required for service installation
- **A config file** — created before installing the service

---

## Step 1 — Place the binary

Copy `retrosync.exe` to a permanent location. A good choice is:

```
C:\ProgramData\RetroSync\retrosync.exe
```

`C:\ProgramData` is available to all users and to the SYSTEM account that runs the service. Avoid placing the binary in a user's home folder or on the Desktop.

---

## Step 2 — Create a config file

Create your `retrosync.toml` in the same folder as the binary:

```
C:\ProgramData\RetroSync\retrosync.toml
```

### Server example

```toml
[node]
port           = 9877
discovery_port = 9876
role           = "server"
name           = "MyServer"

[[sync]]
name  = "snes-saves"
paths = [
    "C:/RetroBat/saves/snes/[*.srm]",
    "C:/RetroBat/saves/snes/libretro.snes9x/[*.state;*.png]",
]
```

### Client example

```toml
[node]
port           = 9877
discovery_port = 9876
role           = "client"
name           = "MyPC"
# server_addr  = "192.168.1.100:9877"  # set this if auto-discovery doesn't work

[[sync]]
name  = "snes-saves"
paths = [
    "C:/RetroBat/saves/snes/[*.srm]",
    "C:/RetroBat/saves/snes/libretro.snes9x/[*.state;*.png]",
]
```

Add additional `[[sync]]` blocks for each system (GBA, N64, etc.) you want to sync.

---

## Step 3 — Install the service

Open a **Command Prompt as Administrator** and run:

```
C:\ProgramData\RetroSync\retrosync.exe -service install -config "C:\ProgramData\RetroSync\retrosync.toml"
```

This registers RetroSync as a Windows service named `RetroSync` that starts automatically at boot.

You can verify it was installed:

```
sc query RetroSync
```

---

## Step 4 — Start the service

```
C:\ProgramData\RetroSync\retrosync.exe -service start
```

Or equivalently using the Windows `sc` command:

```
sc start RetroSync
```

RetroSync is now running. The web UI is available at:

```
http://localhost:9877/ui
```

---

## Managing the service

### Stop

```
C:\ProgramData\RetroSync\retrosync.exe -service stop
```

### Uninstall

Stop the service first, then uninstall:

```
C:\ProgramData\RetroSync\retrosync.exe -service stop
C:\ProgramData\RetroSync\retrosync.exe -service uninstall
```

### Start with all groups paused

If you want the service to start with all sync groups paused (useful to review the state before syncing begins), include `-paused` at install time:

```
C:\ProgramData\RetroSync\retrosync.exe -service install -config "C:\ProgramData\RetroSync\retrosync.toml" -paused
```

The service will start paused on every boot. Use the web UI to unpause when ready.

---

## Log file

When running as a service, RetroSync writes its log to:

```
C:\ProgramData\RetroSync\retrosync.log
```

(Same directory as the binary, unless overridden with `-logfile`.)

To change the log location at install time:

```
C:\ProgramData\RetroSync\retrosync.exe -service install -config "C:\ProgramData\RetroSync\retrosync.toml" -logfile "C:\ProgramData\RetroSync\retrosync.log"
```

---

## Updating RetroSync

1. Stop the service:
   ```
   C:\ProgramData\RetroSync\retrosync.exe -service stop
   ```
2. Replace `retrosync.exe` with the new version.
3. Start the service again:
   ```
   C:\ProgramData\RetroSync\retrosync.exe -service start
   ```

The config file and log file are not affected by a binary update. There is no need to uninstall and reinstall the service unless the install flags need to change.

---

## Managing via Windows Services UI

The service can also be managed through the Windows Services panel (`services.msc`). Look for **RetroSync Sync Service**. From there you can start, stop, or change the startup type using the standard Windows interface.
