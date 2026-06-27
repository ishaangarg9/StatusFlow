#!/usr/bin/env bash
# StatusFlow P12 — install single-node k3s on the hardened box.
#
# Run as the `deploy` user (it uses sudo). Keeps k3s's built-in Traefik as the
# ingress controller (the statusflow chart targets ingressClassName: traefik).
#
# Notes:
#   - --secrets-encryption encrypts Secrets at rest in k3s's datastore.
#   - We KEEP Traefik + servicelb but the box has NO public web ports open (ufw
#     denies inbound 80/443). cloudflared reaches Traefik via its in-cluster
#     ClusterIP, so nothing needs to be exposed on the host.
#   - kubeconfig is written 0600 and copied to ~/.kube/config for the deploy user.
#
#   bash install-k3s.sh
#
set -euo pipefail

# Pin a known-good k3s. Bump deliberately; don't float :latest on a server.
# VERIFY this exact tag exists before running (it hard-fails the installer if not):
#   curl -s https://api.github.com/repos/k3s-io/k3s/releases/latest | grep tag_name
# or override at call time:  K3S_VERSION=vX.Y.Z+k3s1 bash install-k3s.sh
K3S_VERSION="${K3S_VERSION:-v1.31.5+k3s1}"

curl -sfL https://get.k3s.io | \
  INSTALL_K3S_VERSION="$K3S_VERSION" \
  sh -s - server \
    --secrets-encryption \
    --write-kubeconfig-mode 600

# Make kubectl work as the deploy user without sudo.
mkdir -p "$HOME/.kube"
sudo cp /etc/rancher/k3s/k3s.yaml "$HOME/.kube/config"
sudo chown "$(id -u):$(id -g)" "$HOME/.kube/config"
chmod 600 "$HOME/.kube/config"
echo 'export KUBECONFIG=$HOME/.kube/config' >> "$HOME/.bashrc"
export KUBECONFIG="$HOME/.kube/config"

echo "Waiting for the node to be Ready..."
until kubectl get nodes 2>/dev/null | grep -q ' Ready'; do sleep 3; done
kubectl get nodes -o wide

echo
echo "k3s up. metrics-server and Traefik ship with k3s (check: kubectl -n kube-system get deploy)."
echo "Next: setup-postgres-volume.sh (durable DB disk), then sealed-secrets, then helm install."
