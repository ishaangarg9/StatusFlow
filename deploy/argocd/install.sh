#!/usr/bin/env bash
# StatusFlow P13 — bootstrap Argo CD (the GitOps control plane).
#
# This is the ONE imperative step. After this, Git is the source of truth: the
# root app-of-apps (root-app.yaml) pulls deploy/argocd/apps/*, which in turn
# deploy Postgres, StatusFlow, the sealed-secret resources, and cloudflared.
# Argo CD even reconciles its own ingress/params (see config/) so the platform
# is declarative from here on.
#
# Why Argo CD is bootstrapped imperatively and NOT self-installed via Git:
# something has to lay down the first controller. We pin a release manifest,
# apply server params so it serves plain http behind Traefik (TLS terminates at
# Cloudflare), and add the argocd.<domain> Ingress. Everything ELSE is GitOps.
#
# Run on the box with KUBECONFIG pointed at the k3s cluster, AFTER the P12
# bootstrap (sealed-secrets controller installed + `kubectl apply -f sealed/`,
# namespaces statusflow/cloudflared created). See deploy/argocd/README.md.
set -euo pipefail
cd "$(dirname "$0")"

# Pin to a release. VERIFY the tag exists: https://github.com/argoproj/argo-cd/releases
ARGOCD_VERSION="${ARGOCD_VERSION:-v2.13.3}"

echo ">> creating argocd namespace"
kubectl apply -f namespace.yaml

echo ">> installing Argo CD $ARGOCD_VERSION"
kubectl apply -n argocd \
  -f "https://raw.githubusercontent.com/argoproj/argo-cd/${ARGOCD_VERSION}/manifests/install.yaml"

echo ">> applying StatusFlow params: insecure server (Traefik fronts it, edge TLS"
echo "   at Cloudflare) + trimmed footprint for a single node"
kubectl apply -f config/

echo ">> waiting for Argo CD to come up"
kubectl -n argocd rollout status deploy/argocd-server
kubectl -n argocd rollout status deploy/argocd-repo-server
kubectl -n argocd rollout status deploy/argocd-applicationset-controller || true

# Restart the server so server.insecure from the cmd-params CM takes effect.
kubectl -n argocd rollout restart deploy/argocd-server
kubectl -n argocd rollout status deploy/argocd-server

cat <<'EOF'

>> Argo CD is up. Next:

  1) Initial admin password (then change it / disable local admin once SSO/Access is set):
       kubectl -n argocd get secret argocd-initial-admin-secret \
         -o jsonpath='{.data.password}' | base64 -d; echo

  2) If the repo is PRIVATE, register it so Argo can pull (public repos need nothing):
       argocd login argocd.<domain>            # or: kubectl -n argocd port-forward svc/argocd-server 8080:443
       argocd repo add https://github.com/<you>/SaaS.git --username <you> --password <PAT>

  3) Hand the platform to Git — apply the root app-of-apps ONCE:
       kubectl apply -f project.yaml
       kubectl apply -f root-app.yaml

     From now on: edit Git -> PR -> merge -> Argo auto-syncs. Nothing else is kubectl-applied.

  4) Put argocd.<domain> behind Cloudflare Access (email allow-list) before exposing it.
     The Ingress is created by config/ingress.yaml; the tunnel host is pre-wired in
     deploy/cloudflared/configmap.yaml.
EOF
