#!/usr/bin/env bash
# StatusFlow P12 — idempotent host hardening.
#
# Use this if you did NOT pass deploy/vps/cloud-init.yaml at server-create time
# (or want to re-apply). Run as root (or with sudo) on a fresh Ubuntu 24.04 box.
# It is the shell equivalent of cloud-init.yaml and safe to run repeatedly.
#
#   sudo SSH_PUBLIC_KEY="ssh-ed25519 AAAA... you@host" bash harden.sh
#
set -euo pipefail

if [[ $EUID -ne 0 ]]; then echo "run as root (sudo)"; exit 1; fi
: "${SSH_PUBLIC_KEY:?set SSH_PUBLIC_KEY to your public key}"

export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get upgrade -y
apt-get install -y ufw fail2ban unattended-upgrades apt-listchanges

# --- non-root sudo user with your key -------------------------------------
if ! id deploy >/dev/null 2>&1; then
  adduser --disabled-password --gecos "" deploy
  usermod -aG sudo deploy
  echo 'deploy ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/90-deploy
  chmod 440 /etc/sudoers.d/90-deploy
fi
install -d -m 700 -o deploy -g deploy /home/deploy/.ssh
echo "$SSH_PUBLIC_KEY" > /home/deploy/.ssh/authorized_keys
chmod 600 /home/deploy/.ssh/authorized_keys
chown deploy:deploy /home/deploy/.ssh/authorized_keys

# --- ssh: key-only, no root ----------------------------------------------
cat > /etc/ssh/sshd_config.d/99-statusflow-hardening.conf <<'EOF'
PermitRootLogin no
PasswordAuthentication no
KbdInteractiveAuthentication no
ChallengeResponseAuthentication no
X11Forwarding no
MaxAuthTries 3
AllowUsers deploy
EOF

# --- unattended upgrades --------------------------------------------------
cat > /etc/apt/apt.conf.d/51statusflow-unattended <<'EOF'
Unattended-Upgrade::Automatic-Reboot "true";
Unattended-Upgrade::Automatic-Reboot-Time "04:30";
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
EOF

# --- fail2ban -------------------------------------------------------------
cat > /etc/fail2ban/jail.d/statusflow.conf <<'EOF'
[sshd]
enabled = true
mode = aggressive
maxretry = 4
findtime = 10m
bantime = 1h
EOF

# --- sysctl ---------------------------------------------------------------
# br_netfilter must be loaded (and persisted) BEFORE applying the bridge sysctl:
# on a fresh box /proc/sys/net/bridge/bridge-nf-call-iptables doesn't exist yet,
# so `sysctl --system` would exit non-zero and (under set -e) abort the whole
# script before ufw/swap are configured.
modprobe br_netfilter || true
echo br_netfilter > /etc/modules-load.d/br_netfilter.conf
cat > /etc/sysctl.d/99-statusflow.conf <<'EOF'
net.ipv4.conf.all.rp_filter = 1
net.ipv4.conf.default.rp_filter = 1
net.ipv4.tcp_syncookies = 1
net.ipv4.conf.all.accept_redirects = 0
net.ipv6.conf.all.accept_redirects = 0
net.ipv4.conf.all.send_redirects = 0
kernel.kptr_restrict = 1
net.ipv4.ip_forward = 1
net.bridge.bridge-nf-call-iptables = 1
EOF
# Tolerate a still-missing key (e.g. br_netfilter not yet active) — k3s also sets
# the bridge sysctls at runtime; never let this abort hardening.
sysctl --system || true

# --- firewall: deny inbound except SSH (web arrives via Cloudflare Tunnel) -
ufw default deny incoming
ufw default allow outgoing
ufw allow OpenSSH
ufw --force enable

# --- swap -----------------------------------------------------------------
if [[ ! -f /swapfile ]]; then
  fallocate -l 2G /swapfile
  chmod 600 /swapfile
  mkswap /swapfile
  swapon /swapfile
  grep -q '/swapfile' /etc/fstab || echo '/swapfile none swap sw 0 0' >> /etc/fstab
fi

systemctl enable --now fail2ban unattended-upgrades
systemctl restart ssh
echo "Hardening done. Re-connect as deploy@<ip>, then run install-k3s.sh."
