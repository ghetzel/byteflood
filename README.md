# <img src="/ui/img/icon-48.png?raw=true"> Byteflood

## Overview

Byteflood is a peer-to-peer file transfer utility designed to provide reliable
retrieval, synchronization, and management of hundreds of thousands of files
over encrypted connections.

## Building

```
git clone git@github.com:ghetzel/byteflood.git
cd byteflood
make
```

## Running

```
# TODO: this is fiddly and sucks
# First Time
mkdir -p ~/.local/share/byteflood/db
mkdir -p ~/.config/byteflood/keys
cd ~/.config/byteflood/keys
byteflood genkeypair peer

# Sample config.yml
cat <<EOF > ~/.config/byteflood/config2.yml
---
database:
    directories:
    - path: /path/to/files/to/share
      label: share # If unspecified, this will default to the
                   # underscored-lowercase basename of the shared directory

shares:
- name: Share
  filter: label=share # If unspecified, this will default to the
                      # underscored-lowercase version of the share name.
                      #
                      # This can be modified to restrict which files appear under the share
                      # based on any metadata scanned from them.
                      #
local:
    # Only connections to known peers are permitted.  You specify peers you know
    # by associating one or more of their public keys with a name you will recognize.
    known_peers:
        name_you_know_this_person_by:
        - ABCDEFPUBKEY12345...XYZ
        - ZYXWPUBKEY4567890...CBA

        another_person:
        - DEF345ABC123789JK...XYZ
EOF

# Go!
byteflood run

# Go to http://localhost:11984/
#
```
