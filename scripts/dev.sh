#!/usr/bin/env bash
# One-command local stack: Postgres -> migrations -> seed -> api + worker + web,
# all in this one terminal with prefixed, interleaved logs. Ctrl+C tears
# everything down (the trap below kills the whole process group).
#
# Reads .env (git-ignored) for every setting; only OPS_ADDR is overridden for
# the worker so it doesn't try to bind the same port as the api's ops server.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

if [ ! -f .env ]; then
  echo "no .env found — copy .env.example to .env and fill in DATABASE_URL etc. first" >&2
  exit 1
fi

# set -a: every var sourced from .env is exported too, matching what the
# running processes expect (they read straight from the environment).
set -a
# shellcheck disable=SC1091
source .env
set +a

# docker compose ships as either the `docker compose` plugin or the standalone
# `docker-compose` binary depending on the install — try the plugin first.
compose() {
  if docker compose version >/dev/null 2>&1; then
    docker compose "$@"
  else
    docker-compose "$@"
  fi
}

echo "==> Starting Postgres"
compose up -d db

echo "==> Waiting for Postgres"
for _ in $(seq 1 30); do
  if docker exec statusflow-db pg_isready -U postgres >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
docker exec statusflow-db pg_isready -U postgres

echo "==> Running migrations"
migrate -path internal/db/migrations -database "$MIGRATIONS_DATABASE_URL" up

echo "==> Seeding the demo org"
go run ./cmd/seed

# Kill everything in this script's process group on exit — reliable even
# though `go run` and `pnpm dev` fork child processes the direct PID doesn't
# cover, since `kill 0` reaches the whole group, not just the direct children.
trap 'echo; echo "==> Stopping"; kill 0' EXIT INT TERM

echo "==> Starting api, worker, web (Ctrl+C to stop all three)"

go run ./cmd/api 2>&1 | sed -u 's/^/[api]    /' &

# The worker needs its own ops port — sharing the api's would fail to bind.
(
  export OPS_ADDR="${WORKER_OPS_ADDR:-:9091}"
  go run ./cmd/worker 2>&1 | sed -u 's/^/[worker] /'
) &

(
  cd web
  pnpm dev 2>&1 | sed -u 's/^/[web]    /'
) &

wait
