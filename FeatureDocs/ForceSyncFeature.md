# ForceSync Feature

## Status
Implemented

## Description
ForceSync is a command that can be run on any node that is running in client mode. 

When run, it causes the client to:
- For every sync group that the client has defined:
  - Finish any current sync operations if any are happening
  - Pause any future sync operations if not already paused
  - Sync as many files as needed to match the state of the server
  - This includes deleting any local files that match the sync group's file description that are not present on the server
  - Once done, unpause the sync group if it was unpaused before the firce sync (but remain paused if it was paused before the force sync)

  - The sync group web display should have a button for each sync group that will invoke the sync command for that single group
  - The sync group web display should also have a single button that will force sync all of the node's defined groups

To help support this command, we should also add:
- RetroSync should have a command line option to start with all groups paused. This will give us a chance to sync to the server before we have an opportunity to push any files up to the server.
- The web display should have a pause all/unpause all button next to the force sync button that will allow it to pause/unpause all of its defined groups. This should appear on all web displays, regardless of client, server or peer to peer mode
- The server does not have a force-sync button. Only clients have the force sync button
- The force sync button is also not present when operating in peer to peer mode

