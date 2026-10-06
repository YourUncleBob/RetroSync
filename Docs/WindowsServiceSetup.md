# RetroSync on Windows — Running as a Service

Running RetroSync as a Windows service means it starts automatically at boot, runs in the background without a console window, and restarts if the machine reboots — without anyone needing to be logged in.

---

## Prerequisites

- **RetroSync binary** — `retrosync-windows-amd64.exe` (rename to `retrosync.exe` for convenience; `buildall.bat` makes this copy for you, see [Build.md](Build.md))
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

## Firewall rules

A machine running as a **server** must allow inbound connections, otherwise clients cannot reach it. Windows does not add these rules automatically, and a fresh Windows install removes any rules you added earlier. Run these in an **Administrator PowerShell**:

```powershell
# HTTP — clients sync files and fetch the index over this port (required)
New-NetFirewallRule -DisplayName "RetroSync HTTP" -Direction Inbound -Protocol TCP -LocalPort 9877 -Profile Private -Action Allow

# Discovery — lets clients find the server automatically (not needed if clients set server_addr)
New-NetFirewallRule -DisplayName "RetroSync discovery" -Direction Inbound -Protocol UDP -LocalPort 9876 -Profile Private -Action Allow
```

A machine running as a **client** needs no rule for syncing, because it only connects out to the server. However, if the client relies on auto-discovery (no `server_addr` in its config), it must allow inbound UDP 9876 so it can receive the server's discovery broadcasts. Run the UDP rule above on the client in that case, or set `server_addr` and skip it.

Notes:

- Use the ports from `port` and `discovery_port` in your config if you changed them from the defaults.
- The rules above apply only to networks Windows classifies as **Private**. If your network is classified as Public, change it in Settings > Network & internet, or add `Public` to `-Profile`.
- Discovery uses UDP broadcast on each network adapter's own subnet, so the server and client must be on the same subnet. Broadcasts do not cross routers, VPNs, or Wi-Fi networks with client isolation. If a client cannot find the server, set `server_addr` in the client config instead.
- To check connectivity, open `http://<server-ip>:9877/api/status` in a browser on the client machine. JSON means the network path works.
- In the legacy peer-to-peer mode (no `role`), every node needs both rules.

Which rules each machine needs:

| Machine | Inbound rules needed |
|---|---|
| Server | TCP 9877 (required), UDP 9876 (only if clients use auto-discovery) |
| Client with `server_addr` set | None. The client skips discovery and only connects out to the server. |
| Client without `server_addr` | UDP 9876, so it can receive the server's discovery broadcasts |

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
