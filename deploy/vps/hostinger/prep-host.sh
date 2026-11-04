#!/usr/bin/env bash
# StatusFlow — prepare a SHARED Hostinger VPS (Ubuntu 26.04) for k3s.
#
# This is the shared-box counterpart to ../harden.sh. That script assumes a
# fresh, dedicated box (creates a `deploy` user, ufw denies 80/443 because the
# Cloudflare Tunnel is the edge). This box already runs a Docker Compose stack
# (Caddy on 80/443 + n8n) and is already hardened (key-only root SSH, ufw,
# fail2ban, unattended-upgrades), so running harden.sh here would take those
# sites down. This script only adds what k3s needs and leaves the rest alone.
#
# Idempotent — safe to re-run. Run as root:
#   sudo bash prep-host.sh
#
# What it does:
#   1. 4 GiB swapfile (the box ships with none; k3s + Argo + n8n on 8 GiB wants
#      headroom so a burst OOM-kills nothing important). Low swappiness.
#   2. sysctl: inotify limits (k3s/containerd watch a lot of files) + the
#      bridge/forwarding settings pod networking needs.
#   3. ufw: let pod + service traffic flow. ufw's default "deny (routed)" would
#      otherwise silently drop pod egress (the worker's monitor checks) and
#      Caddy -> Traefik. Inbound from the internet stays exactly as it was:
#      22/80/443 only. The k8s API (6443) and Traefik's port are NOT opened.
#   4. /opt/statusflow/backups owned by the Postgres uid (999), for the nightly
#      pg_dump CronJob (deploy/charts/postgres backup.*).
set -euo pipefail

[[ $EUID -eq 0 ]] || { echo "run as root"; exit 1; }

POD_CIDR="${POD_CIDR:-10.42.0.0/16}"       # k3s default --cluster-cidr
SVC_CIDR="${SVC_CIDR:-10.43.0.0/16}"       # k3s default --service-cidr
DOCKER_CIDR="${DOCKER_CIDR:-172.16.0.0/12}" # Docker's default address pools (Caddy lives here)
TRAEFIK_POD_PORT="${TRAEFIK_POD_PORT:-8000}" # Traefik's "web" entrypoint inside its pod
BACKUP_DIR="${BACKUP_DIR:-/opt/statusflow/backups}"

echo ">> swap"
if ! swapon --show | grep -q '/swapfile'; then
  fallocate -l 4G /swapfile
  chmod 600 /swapfile
  mkswap /swapfile
  swapon /swapfile
  grep -q '^/swapfile ' /etc/fstab || echo '/swapfile none swap sw 0 0' >> /etc/fstab
fi
cat > /etc/sysctl.d/90-statusflow.conf <<'EOF'
vm.swappiness = 10
fs.inotify.max_user_instances = 1024
fs.inotify.max_user_watches = 524288
net.ipv4.ip_forward = 1
net.bridge.bridge-nf-call-iptables = 1
net.bridge.bridge-nf-call-ip6tables = 1
EOF
modprobe br_netfilter
echo br_netfilter > /etc/modules-load.d/statusflow.conf
sysctl --system >/dev/null

echo ">> ufw (pod/service networking; public inbound unchanged)"
# Pods -> host (kube-dns, metrics-server, kubelet) and services.
ufw allow from "$POD_CIDR" comment 'k3s pods'
ufw allow from "$SVC_CIDR" comment 'k3s services'
# Routed: pod egress (worker monitor checks, Resend, image pulls) and
# pod <-> pod across the cni0 bridge.
ufw route allow from "$POD_CIDR" comment 'k3s pod egress'
ufw route allow from "$SVC_CIDR" comment 'k3s service egress'
# Caddy (a Docker container) -> Traefik's hostPort. After the hostPort DNAT the
# packet is routed docker-bridge -> cni0, so it needs a ROUTE rule, scoped to
# Docker's ranges and Traefik's one port. Nothing from the internet matches.
ufw route allow proto tcp from "$DOCKER_CIDR" to "$POD_CIDR" port "$TRAEFIK_POD_PORT" comment 'caddy -> traefik'
ufw reload

echo ">> backup dir"
install -d -m 0700 -o 999 -g 999 "$BACKUP_DIR"

echo
echo "Done. ufw now:"
ufw status numbered
echo
echo "Next: bash install-k3s.sh"
