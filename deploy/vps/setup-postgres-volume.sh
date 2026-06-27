#!/usr/bin/env bash
# StatusFlow P12 — put k3s persistent storage on a durable Hetzner Volume.
#
# WHY: the in-cluster Postgres PVC uses k3s's default `local-path` StorageClass,
# which writes under /var/lib/rancher/k3s/storage on the node. By mounting a
# separate Hetzner Volume THERE, the database survives a VPS rebuild/reinstall
# (you keep the Volume, attach it to the new box, re-run k3s) and you don't risk
# filling the root disk. Run this once, AFTER install-k3s.sh and BEFORE the first
# helm install (so no PVC has been provisioned on the root disk yet).
#
# Find the device: `ls -l /dev/disk/by-id/ | grep HC_Volume`
#
#   sudo bash setup-postgres-volume.sh /dev/disk/by-id/scsi-0HC_Volume_XXXXXXXX
#
set -euo pipefail
if [[ $EUID -ne 0 ]]; then echo "run as root (sudo)"; exit 1; fi

DEV="${1:?usage: setup-postgres-volume.sh /dev/disk/by-id/scsi-0HC_Volume_XXXX}"
MNT="/var/lib/rancher/k3s/storage"   # k3s local-path default dir
[[ -e "$DEV" ]] || { echo "device $DEV not found"; exit 1; }

# Format ONLY if the volume has no filesystem yet (never reformat data).
if ! blkid "$DEV" >/dev/null 2>&1; then
  echo "No filesystem on $DEV — creating ext4."
  mkfs.ext4 -F "$DEV"
else
  echo "$DEV already has a filesystem — not touching it."
fi

systemctl stop k3s || true            # quiesce so nothing is writing to the dir
mkdir -p "$MNT"
# Persist by stable by-id path so a device-name change after reboot can't break it.
if ! grep -q "$DEV" /etc/fstab; then
  echo "$DEV $MNT ext4 defaults,nofail,discard 0 2" >> /etc/fstab
fi
mount "$MNT"
df -h "$MNT"
systemctl start k3s

echo "Hetzner Volume mounted at $MNT — local-path PVCs (incl. Postgres) now land on the durable disk."
