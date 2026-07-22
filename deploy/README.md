# StatusFlow deploy (P11 — containerize + local cluster parity)

Helm charts and container images to run StatusFlow on Kubernetes, validated on a
local **kind** cluster so the VPS/k3s bring-up (P12) is boring. This directory is
the source of truth for *how StatusFlow runs in a cluster*; it preserves the
project's isolation invariants (CLAUDE.md §2) end to end.

## What's here

```
deploy/
  charts/
    postgres/    # shared single-node Postgres (StatefulSet + PVC + role/db init)
    statusflow/  # api + worker + web, migration Job (hook), ingress, HPA/PDB, NetworkPolicies
  kind/
    kind-cluster.yaml   # 1-node kind cluster, host :8088 -> ingress :80
    values-local.yaml   # chart overrides for local kind (inert billing, http cookie)
  validate.sh           # build + kind + install + smoke test, one shot
  # --- P12 (VPS + k3s) ---
  vps/             # host bring-up: cloud-init/harden, install-k3s, postgres volume + RUNBOOK
  cloudflared/     # in-cluster Cloudflare Tunnel (no open inbound ports)
  sealed-secrets/  # encrypted secrets in Git (controller + seal.sh)
  prod/            # production chart overrides (existingSecret, secure cookie, Traefik)
  # --- P13 (GitOps) ---
  argocd/          # Argo CD bootstrap + app-of-apps (Git is the source of truth) + RUNBOOK
```

Container images (built from the repo root unless noted):

| Image | Dockerfile | Context | Notes |
|-------|------------|---------|-------|
| `statusflow-api` | `Dockerfile.api` | repo root | distroless, uid 65532 |
| `statusflow-worker` | `Dockerfile.worker` | repo root | distroless, uid 65532 |
| `statusflow-migrate` | `Dockerfile.migrate` | repo root | golang-migrate + baked `internal/db/migrations` |
| `statusflow-web` | `Dockerfile.web` | `web/` | Next.js standalone, uid 1000 |

## The security model the charts enforce (do not regress)

- **Two DB roles, never collapsed.** The migration Job runs on the **privileged
  (superuser) DSN**; api + worker get **only** the restricted `app_user` DSN.
  The privileged DSN lives in its own Secret (`statusflow-migrate`), referenced
  **only** by the Job — never mounted on a long-running pod. Verify:
  `kubectl -n statusflow get deploy statusflow-api -o jsonpath='{.spec.template.spec.containers[0].env[*].name}'`
  shows `DATABASE_URL` and nothing privileged.
- **Migrations run before the app.** The Job is a Helm `pre-install,pre-upgrade`
  hook (weight -5); its Secret is weight -10 so it exists first. An init container
  waits for Postgres so the Job doesn't burn retries on a cold DB.
- **Worker egress is fenced.** `NetworkPolicy/statusflow-worker` allows DB + the
  public internet but **denies** RFC-1918 / link-local (incl. `169.254.169.254`
  metadata) / CGNAT — defence-in-depth behind the in-process SSRF dial guard.
  *(Enforced by a NP-capable CNI: k3s yes; kind's kindnet renders but does not
  enforce — validate there by manifest review.)*
- **Default-deny egress, opt back in per component.** The shared `allow-dns`
  policy selects *every* `app.kubernetes.io/part-of: statusflow` pod, which flips
  them all to **default-deny-egress** — so each component (api, worker, web,
  migrate) needs its own egress policy granting exactly what it talks to (DB,
  internet, the api Service). **Adding a new component with the `part-of` label
  silently loses all egress except DNS until you give it a policy** — this is the
  trap the migration Job hit (no Postgres rule → the pre-install hook hangs on a
  NP-enforcing CNI; invisible on kindnet). When you add a workload, add its
  egress policy in the same PR.
- **The API origin never reaches the browser.** Only `web` (the Next BFF) is on
  the Ingress. The one exception is `/api/stripe/webhook`, routed straight to the
  API because Stripe calls it server-to-server (HMAC-signed, no cookie).
- The ops port (`:9090`, health + `/metrics`) is on the Services for in-cluster
  probes/scraping but **never** on the public Ingress.

## Quick start (local kind)

Prereqs: `docker`, `kind`, `kubectl`, `helm`.

```bash
./deploy/validate.sh        # build, cluster, install, smoke test
# open http://localhost:8088
```

Or step by step:

```bash
kind create cluster --config deploy/kind/kind-cluster.yaml
docker build -f Dockerfile.api     -t statusflow-api:dev     .
docker build -f Dockerfile.worker  -t statusflow-worker:dev  .
docker build -f Dockerfile.migrate -t statusflow-migrate:dev .
docker build -f Dockerfile.web     -t statusflow-web:dev     web
kind load docker-image --name statusflow \
  statusflow-api:dev statusflow-worker:dev statusflow-migrate:dev statusflow-web:dev

kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/main/deploy/static/provider/kind/deploy.yaml
kubectl -n ingress-nginx rollout status deploy/ingress-nginx-controller

kubectl create ns statusflow
helm install pg ./deploy/charts/postgres -n statusflow
helm install statusflow ./deploy/charts/statusflow -n statusflow -f deploy/kind/values-local.yaml
```

Verify:

```bash
kubectl -n statusflow get pods,svc,ingress,netpol
kubectl -n statusflow exec pg-postgres-0 -- \
  psql -U postgres -d statusflow -tAc "select version,dirty from schema_migrations;"   # -> 22|f
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:8088/login                    # -> 200
```

Tear down: `kind delete cluster --name statusflow`.

## Production notes (P12–P13)

> **P12 is built out** — the full VPS + k3s bring-up runbook lives in
> [`vps/README.md`](vps/README.md): provision/harden the Hetzner box, install
> single-node k3s (Traefik kept), put Postgres on a durable Hetzner Volume, seal
> all secrets into Git ([`sealed-secrets/`](sealed-secrets/README.md)), deploy
> with [`prod/`](prod/) overrides, and expose it via a Cloudflare Tunnel
> ([`cloudflared/`](cloudflared/README.md)) with **no open inbound ports**. The
> bullets below are the rationale those artifacts implement.
>
> **P13 is built out** — Argo CD makes **Git the source of truth**
> ([`argocd/README.md`](argocd/README.md)): one imperative bootstrap, then an
> app-of-apps reconciles secrets → Postgres → StatusFlow (migration as a PreSync
> hook) → cloudflared, with prune + self-heal + Git-revision rollback. The §2
> privileged/restricted split and sealed-in-Git secrets carry over unchanged.

- **kind is plain http**, so `values-local.yaml` sets `sessionCookieSecure=false`
  and a `localhost` origin. A real deployment MUST set `config.sessionCookieSecure=true`
  and the real `ingress.host` / `config.appBaseURL` / `web.appOrigin`.
- **Secrets** are rendered inline for dev only. In the cluster set
  `db.secret.create=false`, `migrate.secret.create=false`, `secrets.create=false`
  and supply pre-applied **Sealed Secrets** (P13) carrying `DATABASE_URL`,
  `MIGRATIONS_DATABASE_URL`, and the Stripe/Resend keys. Rotate any password that
  was ever in `.env`/`values` before going public.
- **Postgres** here is a single-node StatefulSet (one DB + `app_user`/superuser
  pair). Pair it with a backup CronJob + a tested restore before it carries real
  data (P14/ops).
- Enable `*.hpa.enabled` / `*.pdb.enabled` once metrics-server is present and you
  run >1 replica.
