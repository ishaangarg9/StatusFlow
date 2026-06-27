#!/usr/bin/env bash
# StatusFlow P12 — install the Sealed Secrets controller.
#
# Bitnami Sealed Secrets lets secrets live in Git *encrypted*: you seal a Secret
# with the controller's public cert, commit the SealedSecret, and only the
# in-cluster controller (holding the private key) can decrypt it into a real
# Secret. Nothing sensitive is ever plaintext in the repo.
#
# In P13 this controller becomes an Argo CD-managed addon; for P12 we install it
# directly so we can seal secrets before the first StatusFlow deploy.
#
# Run with kubectl pointed at the k3s cluster (KUBECONFIG set). Also installs the
# `kubeseal` client locally if missing (needed by seal.sh).
set -euo pipefail

# controller + kubeseal client version (keep in lockstep). VERIFY this tag exists
# first: https://github.com/bitnami-labs/sealed-secrets/releases
SS_VERSION="${SS_VERSION:-v0.27.3}"

echo ">> installing sealed-secrets controller $SS_VERSION into kube-system"
kubectl apply -f "https://github.com/bitnami-labs/sealed-secrets/releases/download/${SS_VERSION}/controller.yaml"
kubectl -n kube-system rollout status deploy/sealed-secrets-controller

if ! command -v kubeseal >/dev/null 2>&1; then
  echo ">> kubeseal client not found — install it to match the controller:"
  echo "   macOS:  brew install kubeseal"
  echo "   linux:  download kubeseal-${SS_VERSION#v}-linux-amd64.tar.gz from the release page"
  echo "   (then re-run seal.sh)"
fi

echo ">> done. Back up the controller's private key so you can restore sealed secrets after a rebuild:"
echo "   kubectl -n kube-system get secret -l sealedsecrets.bitnami.com/sealed-secrets-key -o yaml > sealed-secrets-key.backup.yaml"
echo "   (store that backup OUT of Git — losing it means re-sealing every secret.)"
