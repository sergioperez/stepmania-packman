
#  <img width="32" height="32" alt="packman-logo" src="https://github.com/user-attachments/assets/25eddbca-208e-4bbf-a6f6-e0e16214fd75" /> Stepmania Packman

Stepmania Packman is a tool to manage the packs of your StepMania setup declaratively.

This is meant mainly for kiosk systems, where an user would not plug a mouse and a keyboard.

The current version supports pulling packs from a 
[simfile-pack-search](https://github.com/concubidated/simfile-pack-search) instance.

## Environment vars

`PACKMAN_DIR=/home/stepmania/packman`
Directory where packman stores its configuration files.
This needs to be created before using packman

`SM_PACK_SEARCH_URL="https://url-here.local"`
Simfile-pack-search instance where packman pulls content from: 

`PACK_FOLDER=/home/stepmania/.stepmania/Songs`
Folder where the StepMania packs will be stored
This needs to exist before using packman

`(Optional) DOWNLOAD_PACK_YAML=https://localhost:8000/packs.yaml`
Packman can download `packs.yaml` from a web server, making it simple
to maintain a list in case you do not have another tool to manage `packs.yaml`

## Usage

All the usage is around the `packs.yaml` file.

- Adding a pack: Add a `- id: packID` entry as `- id: 399` on `packs.yaml`

Example:

```
packs:
- id: 399 # Pack name 1
- id: 3666 # Pack name 2
```

You can obtain a pack ID from a `simfile-pack-search` instance.

<img width="928" height="594" alt="image" src="https://github.com/user-attachments/assets/942b6803-6a76-4be4-85dc-6e2aa17cd72c" />

- Removing a pack: Remove the `- id: packID` entry from `packs.yaml`

- Then, run packman

Ideas:

a) Run packman automatically with systemd

b) Push your packs.ini to an HTTP endpoint, and pull it whenever you
start the game

## Installation

There are multiple ways to install this tool. Some examples:

1. Copy it to /usr/bin `cp packman /usr/bin/packman`

2a. Execute the tool within the script that runs your game instance

2b. Modify and use the systemd units under the `install` directory to trigger
packman when packs.yaml is changed

2c. Other - Follow your own

## Troubleshooting information

`pack-status.yaml`  is the "Packman status database". It contains the following yaml:
```
- id: pack_id
  name: pack_name
  status: info
```

The `status` value has the following possible values:
```
pending_delete: Something went wrong during the deletion attempt
deleted_by_user: The user deleted the pack manually
delete_failed: Packman tried to delete the pack, but it failed
installed: The pack got installed successfully in the system
```

The value `status` is not used by Packman, but meant to be a reference to know what happened
for each pack download attempt from the list.

If a download fails => delete the correspondant files from the Songs directory, and the entry in `pack-status.yaml`

## Building

```
go build -o packman app.go
```

## Greetings

Thanks to [@concubidated](https://github.com/concubidated) for the great
project [`simfile-pack-search`](https://github.com/concubidated/simfile-pack-search),
and to all the contributors of the various StepMania-based games and forks,
including StepMania, ITGMania, OutFox, DeadSync, and others.

## Disclaimer

This tool is provided "as is", without warranty of any kind. The author(s)
are not responsible for any damage, data loss, or other issues arising
from its use. Use at your own risk, and review the code before running
it in any environment you care about.
