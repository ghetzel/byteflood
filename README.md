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
cat <<EOF > ~/.config/byteflood/config.yml
---
database:
  uri: 'mysql://byteflood:byteflood@db/byteflood'
EOF

# Go!
byteflood run

# Go to http://localhost:11984/
#
```
