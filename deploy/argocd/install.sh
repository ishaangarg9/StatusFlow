#!/usr/bin/env bash
# StatusFlow — bootstrap Argo CD (the GitOps control plane).
#
# This is the ONE imperative step. After this, Git is the source of truth: the
# root app-of-apps (root-app.yaml) pulls deploy/argocd/apps/*, which in turn
# deploy the sealed-secret resources, Postgres and StatusFlow.
#
# Why Argo CD is bootstrapped imperatively and NOT self-installed via Git:
# something has to lay down the first controller. We pin a release manifest and
# apply server params so it serves plain http (no double TLS behind a proxy).
#
# Footprint (TRIM=1, the default): the box is a shared 8 GiB VPS, so the pieces
# this setup doesn't use are scaled to zero — Dex (no SSO; local admin only),
# the notifications controller, and the ApplicationSet controller (plain
# app-of-apps). Saves ~300-400 MiB. Set TRIM=0 to keep them.
#
# UI access: the UI is NOT published (it's a cluster-admin-grade surface). Reach
# it over SSH:  ssh -L 8080:localhost:8080 root@<box>  then on the box
#   kubectl -n argocd port-forward svc/argocd-server 8080:80
# To publish it instead behind an access proxy, set ARGOCD_HOST=argocd.<domain>
# and this applies config/ingress.yaml with that host.
#
# Run on the box with KUBECONFIG pointed at the k3s cluster, AFTER the sealed-
# secrets controller is installed and the namespaces exist. See
# deploy/vps/hostinger/README.md (or deploy/argocd/README.md).
set -euo pipefail
cd "$(dirname "$0")"

# Pinned. VERIFY the tag exists: https://github.com/argoproj/argo-cd/releases
ARGOCD_VERSION="${ARGOCD_VERSION:-v3.5.6}"
TRIM="${TRIM:-1}"
ARGOCD_HOST="${ARGOCD_HOST:-}"

echo ">> creating argocd namespace"
kubectl apply -f namespace.yaml

echo ">> installing Argo CD $ARGOCD_VERSION"
# Server-side apply: Argo CD 3.x CRDs exceed the client-side
# last-applied-configuration annotation limit.
kubectl apply -n argocd --server-side --force-conflicts \
  -f "https://raw.githubusercontent.com/argoproj/argo-cd/${ARGOCD_VERSION}/manifests/install.yaml"

echo ">> applying StatusFlow params (server.insecure)"
kubectl apply -f config/argocd-cmd-params-cm.yaml
if [[ -n "$ARGOCD_HOST" ]]; then
  echo ">> publishing the UI at $ARGOCD_HOST"
  sed "s/argocd\.<domain>/${ARGOCD_HOST}/" config/ingress.yaml | kubectl apply -f -
fi

if [[ "$TRIM" == "1" ]]; then
  echo ">> trimming unused components (dex, notifications, applicationset)"
  kubectl -n argocd scale deploy argocd-dex-server argocd-notifications-controller \
    argocd-applicationset-controller --replicas=0
fi

echo ">> waiting for Argo CD to come up"
kubectl -n argocd rollout status deploy/argocd-repo-server
# Restart the server so server.insecure from the cmd-params CM takes effect.
kubectl -n argocd rollout restart deploy/argocd-server
kubectl -n argocd rollout status deploy/argocd-server
kubectl -n argocd rollout status statefulset/argocd-application-controller

cat <<'EOT'

>> Argo CD is up. Next:

  1) Initial admin password (change it after first login):
       kubectl -n argocd get secret argocd-initial-admin-secret \
         -o jsonpath='{.data.password}' | base64 -d; echo

  2) The repo is public, so Argo needs no credentials to pull it.

  3) Hand the platform to Git — apply the project + root app-of-apps ONCE:
       kubectl apply -f project.yaml
       kubectl apply -f root-app.yaml

     From now on: edit Git -> PR -> merge -> Argo auto-syncs. Nothing else is kubectl-applied.
EOT
