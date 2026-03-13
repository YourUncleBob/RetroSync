# RetroSync

A golang file sync system that is intended to be used to sync save files between multiple Batocera or Retrobat systems. Needs to run on all systems that will be syncing their files to each other.
I investigates using Syncthing, but ran into issues where the file structure used by Batocera differs slightly from that of Retrobat, making it difficult to share all the save files. I found no good way between Syncthings .stignore file or symbolic links to sync all of these files so that all files were shared between Batocera and Retrobat, but some files were in different folders in Retrobat than they were in Batocera.

## Status
RetroSync is currently a **work in progress in the early test stages**. At this point, it's mostly Claude generated code that I'm in the process of reviewing, testing and modifying. It passes initial tests, but itsn't ready for use. 

## Example problem:

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



## Example config file

### Retrobat
```
# RetroSync configuration file - Retrobat on PC

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
# When copying files down from the server, .srm files will be placed in the snes dfolder and
# .state and .png files in the snes/libretro.snes9x folder
paths = [
    "J:/RetroBat/saves/snes/[*.srm]",
    "J:/RetroBat/saves/snes/libretro.snes9x/[*.state;*.png]"
]
```
### Batocera
```
# RetroSync configuration file — Batocera on any system

[node]
port           = 9877
discovery_port = 9876
role           = "client"

[[sync]]
name  = "snes-saves"
# All .srm,.state and .png files reside in the snes folder
paths = [
    "/userdata/saves/snes/[*.srm;*.state;*.png]",
]
```

## Authoritative Server
Because of my setup at home, it made the most sense for me to use an authoritative server where all clients push changes up to the server and get new files down from the server. This will allow me to do some better conflict resolution (it doesn't exist in the current version) and do things like keep older versions of the save files to restore back to should a save file somehow be damaged.
If desired by others, I could add the ability to run peer-to-peer, like Syncthing does. In general, this was intended to sync smallish files infrequently, so it doesn't attempt to be as failsafe as Syncthing.
I also plan on adding a client-side command to completely refresh all save files from the server, wiping out any local saves.

## Running
retrosync -config *config filename*
example:
    retrosync -config retrosync.toml
The config file is a toml file and is currently required. Will be adding a feature so that config file is auto created if it does not exists

## Building
### On PC from CMD prompt
    PC
    set GOOS=windows&& set GOARCH=amd64&& go build -o dist\retrosync-windows-amd64.exe .
    
    Batocera X86_64
    set GOOS=linux&& set GOARCH=amd64&& go build -o dist\retrosync-linux-amd64.exe .

    Batocera Raspberry PI 5
    set GOOS=linux&& set GOARCH=arm64&& go build -o dist\retrosync-linux-arm64.exe .
### From GitBash Prompt
    PC
    GOOS=windows GOARCH=amd64 go build -o dist/retrosync-windows-amd64.exe .
    
    Batocera X86_64
    GOOS=linux GOARCH=amd64 go build -o dist/retrosync-linux-amd64.exe .
    
    Batocera Raspberry PI 5
    GOOS=linux GOARCH=arm64 go build -o dist/retrosync-linux-arm64.exe .
    
## Web Monitoring/Configuration
Once running, a web UI can be brought up at http://localhost:9877/ui. This shows the status of the system, what it's connected to and all of the current sync groups that are defined. It also allows for new sync groups to be created.
<img width="923" height="907" alt="RetroSyncWeb" src="https://github.com/user-attachments/assets/0b56cc40-bc11-4212-821c-b4d99a2ad9a1" />




## Next Steps
My current plan is:
* Test on multiple platforms (Retrobat PC, Batocera PC, Batocera Raspberry PI 5)
* Add a feature to auto create a config file if none exists
* Add ability to pause individual rules to the web status page
* Currently the syncing doesn't recursively go into folders. It only syncs files directly in specified folders. Add the ability to specify that a path should include recursion.
* Authoritative server support - The idea being that the files on the server can be cleaned up, reduced when there are too many, etc.
  * Add the ability to force a sync from the server (if running in authoritative server mode) to the web status page for any of the clients. I believe this would also remove local files that are not on the server
  * Add ability for server to only get files from clients that are newer than a specified date/time. I want to be able to cleanup the files on the server and don't want older files coming from the clients to be pulled in after that cleanup
* Peer-to-peer support - For my setup I want one of the machines to always be on and act as an authitative server. I want to look at adding support for serverless peer-to-peer to more closely match the Syncthing model to see how difficult that would be
