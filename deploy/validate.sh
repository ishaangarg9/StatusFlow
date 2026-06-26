#!/usr/bin/env bash
# P11 local cluster parity check: build the four images, stand up a kind cluster,
# install Postgres + StatusFlow, and smoke-test the full ingress -> web BFF ->
# api -> Postgres(app_user) path. Idempotent enough to re-run; `kind delete
# cluster --name statusflow` to tear down.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"
NS=statusflow
CLUSTER=statusflow

echo "==> Building images"
docker build -f Dockerfile.api     -t statusflow-api:dev     --build-arg VERSION=p11-local .
docker build -f Dockerfile.worker  -t statusflow-worker:dev  --build-arg VERSION=p11-local .
docker build -f Dockerfile.migrate -t statusflow-migrate:dev .
docker build -f Dockerfile.web     -t statusflow-web:dev     web

echo "==> Creating kind cluster (if absent)"
kind get clusters | grep -qx "$CLUSTER" || kind create cluster --config deploy/kind/kind-cluster.yaml

echo "==> Loading images into kind"
kind load docker-image --name "$CLUSTER" \
  statusflow-api:dev statusflow-worker:dev statusflow-migrate:dev statusflow-web:dev

echo "==> Installing ingress-nginx (kind provider)"
kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/main/deploy/static/provider/kind/deploy.yaml
kubectl -n ingress-nginx rollout status deploy/ingress-nginx-controller --timeout=180s

echo "==> Installing Postgres"
kubectl get ns "$NS" >/dev/null 2>&1 || kubectl create ns "$NS"
helm upgrade --install pg ./deploy/charts/postgres -n "$NS"
kubectl -n "$NS" rollout status statefulset/pg-postgres --timeout=180s

echo "==> Installing StatusFlow (migration Job runs as a pre-install hook)"
helm upgrade --install statusflow ./deploy/charts/statusflow -n "$NS" \
  -f deploy/kind/values-local.yaml --timeout 240s
kubectl -n "$NS" rollout status deploy/statusflow-api    --timeout=120s
kubectl -n "$NS" rollout status deploy/statusflow-web    --timeout=120s
kubectl -n "$NS" rollout status deploy/statusflow-worker --timeout=120s

echo "==> Smoke test (ingress -> web BFF -> api -> Postgres)"
code=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8088/login)
echo "GET /login -> HTTP $code"; [ "$code" = "200" ] || { echo "FAIL: /login"; exit 1; }
# Retry signup a few times: on an upgrade that rolled the api pod, the long-lived
# web (BFF) pod can hold a keep-alive connection to the terminated api and 500 the
# first request before re-dialing. A racy single-shot probe should not fail the run.
signup_ok=""
for attempt in 1 2 3 4 5; do
  email="smoke-$(date +%s)-$attempt@example.com"
  out=$(curl -s -w '\n%{http_code}' -X POST http://localhost:8088/bff/auth/signup \
    -H "Content-Type: application/json" -H "Origin: http://localhost:8088" \
    -d "{\"email\":\"$email\",\"password\":\"correcthorsebatterystaple\",\"name\":\"Smoke\"}")
  status=$(echo "$out" | tail -1)
  echo "POST /bff/auth/signup (attempt $attempt) -> $status"
  if [ "$status" = "201" ]; then signup_ok=1; break; fi
  sleep 2
done
[ -n "$signup_ok" ] || { echo "FAIL: signup"; exit 1; }

echo "==> OK. App at http://localhost:8088"
