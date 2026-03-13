# RetroSync

A golang file sync system that is intended to be used to sync save files between multiple Batocera or Retrobat systems. Needs to run on all systems that will be syncing their files to each other.
I investigated using Syncthing, but ran into issues where the file structure used by Batocera differs slightly from that of Retrobat, making it difficult to share all the save files. I found no good way between Syncthings .stignore file or symbolic links to sync all of these files so that all files were shared between Batocera and Retrobat, but some files were in different folders in Retrobat than they were in Batocera.

## Status
RetroSync is currently working and has been tested with Windows PC Retrobat systems. I have built it for the Batocera systems, but have yet to deploy and test it. I believe it is feature complete and ready for use.

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
I did all development in JetBrains GoLand on a Windows PC. I believe this can be built on any platform that supports golang, but I've only tried it from Windows.

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
<img alt="RetroSyncWeb" src="https://github.com/YourUncleBob/RetroSync/blob/main/images/RetroSyncWeb.png" />


## Next Steps
My current plan is:
* Look into deployment methods. On Windows, should this be setup as a service? I don't know what that looks like on a Batocera system or how to deploy and run it there. I've also never tried a web browser on Batocera before. I assume RetroSync's web view will work there, but haven't tried it.
* Test on Batocera platforms (I have access to Batocera PC, Batocera Raspberry PI 5)
* Currently the syncing doesn't recursively go into folders. It only syncs files directly in specified folders. Add the ability to specify that a path should include recursion.
* Peer-to-peer support - For my setup I want one of the machines to always be on and act as an authitative server. I want to look at adding support for serverless peer-to-peer to more closely match the Syncthing model to see how difficult that would be. I believe peer-to-peer is working (my first stab at RetroSync was peer-to-peer, I believe it still works), but it is untested.

