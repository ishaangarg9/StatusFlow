#!/usr/bin/env bash
# StatusFlow P12 — seal all cluster secrets from secrets.env into sealed/.
#
# Reads ./secrets.env (gitignored), builds each Kubernetes Secret, and pipes it
# through `kubeseal` so only the in-cluster controller can decrypt it. The output
# SealedSecrets in ./sealed/ are encrypted and SAFE to commit.
#
# Prereqs: KUBECONFIG points at the cluster; install.sh has run; kubeseal present.
#
#   cd deploy/sealed-secrets && cp secrets.env.example secrets.env && $EDITOR secrets.env
#   ./seal.sh
#
# The Secret NAMES below MUST match what the charts/manifests reference:
#   statusflow-db        -> values-statusflow.yaml  db.secret.existingSecret
#   statusflow-migrate   -> values-statusflow.yaml  migrate.secret.existingSecret
#   statusflow-app       -> values-statusflow.yaml  secrets.existingSecret
#   statusflow-pg-auth   -> values-postgres.yaml    auth.existingSecret
#   cloudflared-credentials (ns cloudflared) -> cloudflared deployment volume
set -euo pipefail
cd "$(dirname "$0")"

[[ -f secrets.env ]] || { echo "secrets.env missing — cp secrets.env.example secrets.env and fill it"; exit 1; }
# shellcheck disable=SC1091
source ./secrets.env
command -v kubeseal >/dev/null || { echo "kubeseal not found — see install.sh"; exit 1; }

: "${POSTGRES_SUPERUSER_PASSWORD:?set in secrets.env}"
: "${APP_USER_PASSWORD:?set in secrets.env}"

NS="${NS:-statusflow}"
PG_HOST="${PG_HOST:-pg-postgres}"          # postgres Service (release "pg")
SEAL=(kubeseal --controller-name sealed-secrets-controller --controller-namespace kube-system --format yaml)
mkdir -p sealed

# In-cluster DSNs. sslmode=disable: same-node pod-to-pod, matching the chart default.
APP_DSN="postgres://app_user:${APP_USER_PASSWORD}@${PG_HOST}:5432/statusflow?sslmode=disable"
MIG_DSN="postgres://postgres:${POSTGRES_SUPERUSER_PASSWORD}@${PG_HOST}:5432/statusflow?sslmode=disable"

mk() {  # mk <secret-name> <namespace> <kubectl-create-args...>
  local name="$1" ns="$2"; shift 2
  kubectl create secret generic "$name" -n "$ns" "$@" \
    --dry-run=client -o yaml | "${SEAL[@]}" > "sealed/${name}.yaml"
  echo "  sealed/${name}.yaml"
}

echo ">> sealing into ./sealed/"

# Postgres roles (consumed by the postgres chart's existingSecret).
mk statusflow-pg-auth "$NS" \
  --from-literal=superuser-password="$POSTGRES_SUPERUSER_PASSWORD" \
  --from-literal=app-user-password="$APP_USER_PASSWORD"

# app_user DSN -> api + worker.
mk statusflow-db "$NS" --from-literal=DATABASE_URL="$APP_DSN"

# privileged DSN -> migration Job ONLY.
mk statusflow-migrate "$NS" --from-literal=MIGRATIONS_DATABASE_URL="$MIG_DSN"

# Stripe + Resend (empty values keep billing/email inert).
mk statusflow-app "$NS" \
  --from-literal=RESEND_API_KEY="${RESEND_API_KEY:-}" \
  --from-literal=STRIPE_SECRET_KEY="${STRIPE_SECRET_KEY:-}" \
  --from-literal=STRIPE_WEBHOOK_SECRET="${STRIPE_WEBHOOK_SECRET:-}" \
  --from-literal=STRIPE_PRICE_ID="${STRIPE_PRICE_ID:-}"

# Cloudflare Tunnel credentials (separate namespace).
if [[ -n "${TUNNEL_CREDENTIALS_FILE:-}" && -f "$TUNNEL_CREDENTIALS_FILE" ]]; then
  mk cloudflared-credentials cloudflared \
    --from-file=credentials.json="$TUNNEL_CREDENTIALS_FILE"
else
  echo "  (skipping cloudflared-credentials: set TUNNEL_CREDENTIALS_FILE to the tunnel JSON)"
fi

echo ">> done. Commit ./sealed/*.yaml (encrypted). NEVER commit secrets.env."
