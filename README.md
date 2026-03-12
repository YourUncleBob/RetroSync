# RetroSync

Primarily used to sync save files between multiple Batocera or Retrobat systems.
I investigates using Syncthing, but ran into issues where the file structure used by Batocera differs slightly from that of Retrobat, making it difficult to share all the save files. I found no good way between Syncthings .stignore file or symbolic links to sync all of these files so that all files were shared between Batocera and Retrobat, but some files were in different folders in Retrobat than they were in Batocera.

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
    "J:/RetroBat/saves/snes/libretro.snes9x/[*.state;*.png]",
]
```
### Batocera
```
# RetroSync configuration file — Batocera on any system

[node]
port           = 9877
discovery_port = 9876
role           = "client"
server_addr    = "192.168.X.X:9877"

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
