#!/bin/bash
# App-authored (not from jnovack/cloudkey) -- formats and mounts a bulk-
# storage drive at /volume: whole-disk ext4, no partition table (the
# "superfloppy" pattern every drive tried on this hardware so far has used).
#
# Wipes the target device unconditionally, with no attempt to detect or
# preserve an existing filesystem/partition table -- a drive being
# (re)installed here is never assumed to carry data worth keeping. Don't run
# this against a drive you haven't confirmed is the one you mean to wipe.
#
# CRITICAL, hard-won detail this script preserves on purpose: the mount is
# persisted via a systemd .mount unit, NOT /etc/fstab. This board runs a live
# UniFi bootup-hook framework (owned by the load-bearing base-files package --
# cloudkey-plus-apq8053-base-files / cloudkey-g2-apq8053-base-files by model --
# and ubnt-tools, both deliberately kept installed, never purged) that resets
# /etc/fstab to a minimal template on every boot, silently dropping any
# manually-added line. A plain systemd unit file under /etc/systemd/system/ is
# untouched by that reset and survives reboot correctly -- confirmed by direct
# testing against this exact hardware. Writing to /etc/fstab here would look
# correct immediately and then silently stop mounting after the next reboot.
#
# Usage: phase1-format-mount-volume.sh <device> [-y]
#   <device>  e.g. /dev/sda -- required, no default, so a typo in the
#             calling context can't silently target the wrong disk.
#   -y        skip the confirmation prompt (for non-interactive runs).
#
# Safe to re-run against the same already-formatted device: writing the
# same unit file content twice and re-enabling it is a no-op.

set -euo pipefail

DEVICE="${1:?Usage: $0 <device> [-y]}"
ASSUME_YES="${2:-}"

if [[ ! -b "$DEVICE" ]]; then
  echo "error: $DEVICE is not a block device" >&2
  exit 1
fi

lsblk "$DEVICE"

if [[ "$ASSUME_YES" != "-y" ]]; then
  read -rp "This will WIPE $DEVICE and reformat it as /volume. Continue? [y/N] " reply
  [[ "$reply" =~ ^[Yy]$ ]] || { echo "aborted"; exit 1; }
fi

wipefs -a "$DEVICE"
mkfs.ext4 -F -L volume "$DEVICE"

UUID="$(blkid -s UUID -o value "$DEVICE")"

cat > /etc/systemd/system/volume.mount <<EOF
[Unit]
Description=Bulk storage drive (whole-disk ext4, no partition table)

[Mount]
What=/dev/disk/by-uuid/$UUID
Where=/volume
Type=ext4
Options=defaults,noatime

[Install]
WantedBy=local-fs.target
EOF

systemctl daemon-reload
systemctl enable --now volume.mount

echo "Done. /volume is mounted:"
lsblk "$DEVICE"
mount | grep /volume
