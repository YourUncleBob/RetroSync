# Feature: Google Drive Sync

## Stutus
Not yet implemented

## Overview

Add Google Drive as an optional cloud sync target for RetroSync. Google sync is a node type like client, server and P2P. It is a configuration option in the Node's role field. In Google sync mode, files are pushed up when the local copy is newer and pulled down when the Drive copy is newer, using the same mod-time / hash comparison logic that governs LAN sync today.

This allows save files to roam between machines even when they are not on the same network, and provides an off-site backup as a side effect.

---

## How Files Are Stored on Drive

Files are stored under a single root folder (configurable, default `RetroSync`) using the virtual path as the subfolder structure:

```
RetroSync/
  groupname/
    filename.srm
    filename.sav
  another-group/
    save.srm
```

This mirrors the `groupname/filename` virtual path format used internally, so the same routing logic that maps virtual paths to local directories applies unchanged when files arrive from Drive.

---

## Credentials

### How credentials work

RetroSync uses Google's OAuth2 protocol to access Drive. Each user creates their own Google Cloud project and generates their own `credentials.json`. This means every user has full ownership of their own API access, with no dependency on the RetroSync developer managing a shared project or approving accounts.

There are two credential files involved:

- **`credentials.json` (per-user, created once)** — identifies the user's own Google Cloud application to Google. Contains a `client_id` and `client_secret`. Created by the user in the Google Cloud Console following the steps below. The same `credentials.json` can be used on all machines belonging to the same user. Its path is set in the RetroSync config and loaded at startup.

- **`token.json` (per-user, generated automatically)** — created by RetroSync after the user completes the one-time consent flow using their `credentials.json`. Contains the access and refresh tokens that authorise RetroSync to access their Drive. Should be treated as a secret. Can be copied to other machines to avoid repeating the consent flow.

### Creating your credentials.json

Each user must do this once. It takes about 5 minutes.

1. Go to [https://console.cloud.google.com](https://console.cloud.google.com) and sign in with the Google account whose Drive you want to sync to.
2. Click the project dropdown at the top and create a new project. The name doesn't matter — `RetroSync` is fine.
3. Go to **APIs & Services → Library**, search for **Google Drive API**, and click **Enable**.
4. Go to **APIs & Services → OAuth consent screen**.
   - Choose **External** and click **Create**.
   - Fill in any app name (e.g. `RetroSync`), set your email as the support email and developer contact. Leave everything else blank.
   - Click through the Scopes screen without adding anything.
   - On the **Test users** screen, add your own Google account email, then click **Save and Continue**.
5. Go to **APIs & Services → Credentials**.
   - Click **+ Create Credentials → OAuth client ID**.
   - Choose **Desktop app** as the application type and click **Create**.
6. Click **Download JSON** and save the file as `credentials.json` somewhere RetroSync can read it (e.g. `C:/ProgramData/RetroSync/credentials.json`).

The Google Cloud project can stay in Testing status indefinitely for personal use — there is no need to publish or verify the app since you are both the developer and the only user of your own project.

---

## Configuration

Google Drive sync is configured by setting `role = "googledrive"` in the `[node]` section. When a node has this role it does not participate in LAN discovery or LAN sync at all — it only syncs with Google Drive. A new top-level `[drive]` section provides the Drive-specific settings:

```toml
[node]
role = "googledrive"

[drive]
credentials_file  = "C:/ProgramData/RetroSync/credentials.json"
token_file        = "C:/ProgramData/RetroSync/drive-token.json"
root_folder       = "RetroSync"   # folder name on Drive (created if absent)
upload_interval   = 30            # seconds between upload timer checks (default 30)
download_interval = 600           # seconds between Drive poll cycles (default 600)
```

- `credentials_file` — path to the `credentials.json` file created via the steps above. The same file can be used on all machines belonging to the same Google account.
- `token_file` — where RetroSync stores the user token after the initial consent flow. Created automatically on first auth. Can be copied to other machines to avoid repeating the flow. Keep this file secret.
- `root_folder` — the name of the top-level folder created on Drive. All machines pointing at the same Drive account and root folder name will share files.
- `upload_interval` — how often the upload timer fires to catch any files that were not uploaded immediately via fsnotify, in seconds. Defaults to 30. In practice most uploads happen within seconds of a file change being detected.
- `download_interval` — how often RetroSync polls Drive for files newer than the local copy, in seconds. Defaults to 600 (10 minutes). This is intentionally infrequent since Drive polling consumes API quota. A download pass also always runs immediately on startup.

Sync applies to all configured sync groups. There is no per-group opt-in/opt-out in the initial implementation.

---

## Authentication

RetroSync uses OAuth2 with offline access (a refresh token) so it can sync without user interaction after the first setup.

### First-time setup

Run the following command once before starting RetroSync normally (or before installing it as a service):

```
retrosync.exe -drive-auth -config retrosync.toml
```

RetroSync will print a URL to the console:

```
[retrosync] Google Drive auth required.
Open this URL in a browser and paste the code here:
  https://accounts.google.com/o/oauth2/auth?...

Enter code:
```

Visit the URL in a browser, sign in with the Google account whose Drive you want to use, and grant RetroSync permission. Paste the authorisation code back into the terminal. RetroSync saves the token to `token_file`. All subsequent runs — including when running as a service — load and refresh the token automatically without user interaction.

The `token.json` file can be copied to other machines running RetroSync under the same Google account, so the consent flow only needs to be completed once per account.

### Running as a Windows Service

Because a service has no console, `-drive-auth` must be run interactively before installing the service:

```
retrosync.exe -drive-auth -config retrosync.toml
retrosync.exe -service install -config retrosync.toml
retrosync.exe -service start
```

---

## Sync Behaviour

### Hash comparison

RetroSync uses MD5 checksums internally, and the Drive API v3 also returns an MD5 checksum (`md5Checksum` field) for each file. Because both sides use the same algorithm, the hash returned by Drive can be compared directly against the hash in the local file index with no conversion or re-hashing required. A file is considered changed if the hashes differ. Modification time is used as the tiebreaker when hashes differ, in the same way as LAN sync.

### Upload behaviour (local → Drive)

Uploads are driven by two triggers:

1. **On file change** — when fsnotify detects a write or create event on a watched file (subject to the existing 500ms debounce), the changed file is uploaded to Drive immediately, without waiting for the next upload timer tick. This ensures that any save file written on this machine reaches Drive within a few seconds.
2. **On upload timer** — every `upload_interval` seconds (default 30), RetroSync does a pass over all local files and uploads any whose local mod time is newer than the last known Drive mod time for that file. This catches any files that may have been missed (e.g. if Drive was temporarily unreachable).

### Download behaviour (Drive → local)

Downloads are driven by two triggers:

1. **On startup** — immediately after RetroSync starts, before any sync timers fire, it performs a full download pass against Drive. This ensures that any changes made on another machine while this machine was off are pulled down right away.
2. **On download timer** — every `download_interval` seconds (default 600), RetroSync lists all files in the Drive root folder and downloads any whose Drive mod time is newer than the local copy. This is intentionally infrequent since Drive polling consumes API quota and cross-machine sync via Drive is not latency-sensitive.

The startup download pass runs before the upload timer starts, so the local file index is up to date before any uploads occur.

Paused groups are skipped for both upload and download, consistent with LAN sync behaviour.

### Conflict resolution

Same as LAN sync: the newer mod time wins. There is no three-way merge. If a file was modified on two machines between download timer ticks, the one with the later mod time will overwrite the other on the next download pass.

### No LAN sync

A node with `role = "googledrive"` does not start UDP discovery, does not connect to any LAN server or peers, and does not run the LAN sync loop. Its only network activity is with the Google Drive API. This makes it suitable for machines that are connected to the internet but not to the same LAN as other RetroSync nodes.

---

## Web UI Changes

- The Node Info panel shows `Mode: Google Drive` in the Mode field.
- The Connected Peers panel is hidden (not applicable in this mode).
- A **Drive Status** section shows: last upload time, last download time, and last error (if any).
- Pausing a group suppresses Drive sync for that group.
- Drive sync events (uploads and downloads) appear in the Sync Log with a peer name of `Google Drive`.

---

## New Package

All Drive logic lives in a new `internal/drive` package to keep the existing packages unchanged. The package exposes a `DriveSync` type with `Start()` and `Stop()` methods. When `role = "googledrive"`, the node skips LAN discovery and the LAN sync loop entirely, and instead wires up a `DriveSync` instance in their place. `DriveSync` receives a snapshot of the current file index and the `routeIncoming` function — the same inputs used by the LAN sync loops today.

---

## Out of Scope for Initial Implementation

- Per-group Drive enable/disable.
- Shared Drive / Team Drive support.
- Selective sync (syncing only groups that exist on Drive already).
- Drive-side delete propagation (if a file is deleted on Drive, it is not deleted locally — Drive is treated as an additive source only, not authoritative like a ForceSync).
- A UI-based auth flow (browser auto-open). The terminal/log code-paste flow is sufficient for v1.
