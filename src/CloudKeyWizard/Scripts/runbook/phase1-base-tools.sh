#!/bin/bash
# App-authored (not from jnovack/cloudkey) -- installs packages later
# steps in this app, and general day-to-day use of the box, depend on but
# that aren't in the stock UniFi OS image.
#
#   rsync            -- file sync (e.g. media/download offload to an NFS share)
#   nfs-common        -- NFS client (mount.nfs / mount.nfs4)
#   curl              -- confirmed missing from the stock image; several other
#                        bundled scripts already had to work around this
#                        individually (command -v curl || apt-get install curl)
#                        before this closed the gap once for everyone
#   wget              -- commonly assumed alongside curl
#   git               -- near-zero footprint, commonly expected on a real box
#   nano              -- the stock image typically only has busybox vi
#   unzip             -- commonly needed, tiny
#   ca-certificates   -- so curl/wget/apt over HTTPS actually work cleanly,
#                        not just "installed"
#   tmux              -- persistent sessions matter once this is a real
#                        general-purpose box people SSH into directly, not
#                        just through this app
#   htop              -- nicer terminal-side process view
#
# Deliberately NOT included: build-essential (heavy, most people won't
# compile anything on this specific low-power ARM box -- a deliberate
# opt-in if ever needed, not a default), net-tools (legacy; ip/iproute2
# already covers this and is already present).
#
# Safe to re-run: apt-get install is idempotent, so this doubles as a
# "confirm the baseline is present" check.
set -euo pipefail

PACKAGES="rsync nfs-common curl wget git nano unzip ca-certificates tmux htop"

apt-get update

echo "== simulate (expect only additions, nothing removed) =="
apt-get install --simulate $PACKAGES

echo "== install =="
DEBIAN_FRONTEND=noninteractive apt-get install -y $PACKAGES

echo "== verify =="
command -v rsync
command -v mount.nfs
command -v curl
command -v wget
command -v git
command -v nano
command -v unzip
command -v tmux
command -v htop
echo "base tooling present"
