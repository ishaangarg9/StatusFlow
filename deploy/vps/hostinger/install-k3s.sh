#!/usr/bin/env bash
# StatusFlow — single-node k3s on the SHARED Hostinger box, behind the existing
# Dockerised Caddy. Shared-box counterpart to ../install-k3s.sh.
#
# Edge model on this box (differs from the Hetzner/Tunnel runbook):
#
#   internet :443 -> Caddy (Docker, owns 80/443, auto-HTTPS)
#                 -> Traefik hostPort on the docker0 bridge IP (private, :8080)
#                 -> web (BFF) Service -> api
#
#   - --disable=servicelb: k3s's klipper-lb would otherwise bind Traefik to the
#     host's 0.0.0.0:80/443 and fight Caddy for them.
#   - Traefik's Service becomes ClusterIP and its `web` entrypoint is published
#     ONLY as a hostPort on the docker0 bridge address (default 172.17.0.1). That
#     address is not routable from the internet, so Traefik is reachable from
#     Caddy's container and nothing else (ufw still denies everything but
#     22/80/443 inbound; see prep-host.sh for the one scoped route rule).
#   - forwardedHeaders.trustedIPs: Traefik keeps Caddy's X-Forwarded-For instead
#     of overwriting it with Caddy's container IP. Without this the api's per-IP
#     login limiter would see one client (Caddy) for the whole internet.
#   - --secrets-encryption: Secrets encrypted at rest in k3s's datastore.
#
# Run as root AFTER prep-host.sh:
#   sudo bash install-k3s.sh
set -euo pipefail

[[ $EUID -eq 0 ]] || { echo "run as root"; exit 1; }

# Pinned. Bump deliberately; verify the tag exists first:
#   curl -s https://api.github.com/repos/k3s-io/k3s/releases | grep tag_name | head
K3S_VERSION="${K3S_VERSION:-v1.36.5+k3s1}"
TRAEFIK_BIND_IP="${TRAEFIK_BIND_IP:-$(ip -4 -o addr show docker0 | awk '{print $4}' | cut -d/ -f1)}"
TRAEFIK_HOST_PORT="${TRAEFIK_HOST_PORT:-8080}"

[[ -n "$TRAEFIK_BIND_IP" ]] || { echo "could not detect docker0's IP; set TRAEFIK_BIND_IP"; exit 1; }
echo ">> Traefik will listen on ${TRAEFIK_BIND_IP}:${TRAEFIK_HOST_PORT} (docker bridge only)"

# The HelmChartConfig must exist before k3s first deploys Traefik so the very
# first rollout already has the private-only bind (no window where it's on :80).
install -d /var/lib/rancher/k3s/server/manifests
cat > /var/lib/rancher/k3s/server/manifests/traefik-config.yaml <<EOF
apiVersion: helm.cattle.io/v1
kind: HelmChartConfig
metadata:
  name: traefik
  namespace: kube-system
spec:
  valuesContent: |-
    service:
      type: ClusterIP
    ports:
      web:
        hostPort: ${TRAEFIK_HOST_PORT}
        hostIP: ${TRAEFIK_BIND_IP}
        forwardedHeaders:
          trustedIPs:
            - 172.16.0.0/12
            - 10.42.0.0/16
      websecure:
        expose:
          default: false
    # Argo CD only calls an Ingress Healthy once status.loadBalancer has an
    # address. With servicelb off and a ClusterIP Service there's nothing for
    # publishedService to copy, so every Ingress stays Progressing and Argo
    # syncs hang on "waiting for healthy state of Ingress" (PostSync hooks never
    # run). Publish the private address Caddy actually dials instead.
    providers:
      kubernetesIngress:
        publishedService:
          enabled: false
        ingressEndpoint:
          ip: ${TRAEFIK_BIND_IP}
    # Traefik chart >= 40 also renders hostIP into the entrypoint's listen
    # address (--entryPoints.web.address=<hostIP>:8000), which doesn't exist
    # inside the pod and crash-loops it ("cannot assign requested address").
    # additionalArguments render after the generated ones and win, so the pod
    # listens on :8000 while the hostPort mapping above stays bound to hostIP.
    additionalArguments:
      - "--entryPoints.web.address=:8000/tcp"
    # One node + a hostPort: a surge pod can't schedule while the old one holds
    # the port, so the default rolling update deadlocks. Replace in place.
    updateStrategy:
      type: RollingUpdate
      rollingUpdate:
        maxUnavailable: 1
        maxSurge: 0
EOF

curl -sfL https://get.k3s.io | \
  INSTALL_K3S_VERSION="$K3S_VERSION" \
  sh -s - server \
    --disable=servicelb \
    --secrets-encryption \
    --write-kubeconfig-mode 600

mkdir -p "$HOME/.kube"
cp /etc/rancher/k3s/k3s.yaml "$HOME/.kube/config"
chmod 600 "$HOME/.kube/config"
grep -q 'KUBECONFIG=' "$HOME/.bashrc" || echo 'export KUBECONFIG=$HOME/.kube/config' >> "$HOME/.bashrc"
export KUBECONFIG="$HOME/.kube/config"

echo ">> waiting for the node"
until kubectl get nodes 2>/dev/null | grep -q ' Ready'; do sleep 3; done
echo ">> waiting for Traefik"
until kubectl -n kube-system get deploy traefik >/dev/null 2>&1; do sleep 3; done
kubectl -n kube-system rollout status deploy/traefik --timeout=300s
kubectl get nodes -o wide

echo
echo ">> Traefik probe from the host (expect 404 — no Ingress yet):"
curl -s -o /dev/null -w '%{http_code}\n' "http://${TRAEFIK_BIND_IP}:${TRAEFIK_HOST_PORT}/" || true
echo ">> And from the public IP it must NOT answer (run from your laptop):"
echo "     curl -m 5 http://<vps-ip>:${TRAEFIK_HOST_PORT}/   # expect timeout"
