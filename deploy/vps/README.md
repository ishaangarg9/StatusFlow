# P12 — VPS + k3s bring-up (runbook)

Take the P11 charts (validated on kind) live on a single **Hetzner box running
k3s**, reachable **only** through a **Cloudflare Tunnel** (no open inbound web
ports), with every secret **sealed in Git**. The isolation invariants (CLAUDE.md
§2) carry over unchanged: the migration Job gets the privileged DSN, api/worker
get only `app_user`, the worker egress NetworkPolicy holds, the cookie is
`Secure`, and only the BFF (+ the Stripe webhook) is on the ingress.

This directory holds the **host** scripts; the edge lives in `deploy/cloudflared/`,
secrets in `deploy/sealed-secrets/`, and prod chart overrides in `deploy/prod/`.

```
deploy/vps/
  cloud-init.yaml             # paste as Hetzner user-data at create time
  harden.sh                   # same hardening as a re-runnable script
  install-k3s.sh              # single-node k3s (Traefik kept, secrets-encryption)
  setup-postgres-volume.sh    # mount Hetzner Volume under k3s local-path
```

## 0. Prereqs (the §8 open items)

Confirm before you start: a **domain** on Cloudflare; a **Hetzner** account +
region; **Resend** domain verified (or skip → outbox); **Stripe test** keys (or
skip → billing inert). Pick a box: **CX42 (16 GB)** budget / **CX52 (32 GB)**
comfortable — see `context-summary.md` §3.

## 1. Provision + harden the box

Create the server (Ubuntu 24.04) and a **Volume** (~20–50 GB) for Postgres data.
Paste `cloud-init.yaml` as *user data* (replace `SSH_PUBLIC_KEY_HERE` first).
Lock SSH to your IP with the Hetzner **cloud firewall** too (belt + braces with
ufw). Then:

```bash
ssh deploy@<server-ip>      # root login + passwords are already disabled
```

Didn't use cloud-init? `sudo SSH_PUBLIC_KEY="ssh-ed25519 AAAA… you" bash harden.sh`.

What you get: non-root `deploy` sudoer, key-only SSH, ufw (deny inbound except
SSH — **no 80/443**, traffic comes via the tunnel), unattended-upgrades,
fail2ban, swap, sysctl hardening.

## 2. Install k3s + durable DB disk

```bash
bash install-k3s.sh                                   # Traefik + metrics-server ship with k3s
sudo bash setup-postgres-volume.sh /dev/disk/by-id/scsi-0HC_Volume_XXXX
```

`setup-postgres-volume.sh` mounts the Hetzner Volume at k3s's local-path dir, so
the Postgres PVC lands on a disk that survives a box rebuild. Run it **before**
the first `helm install`.

## 3. Seal the secrets

On the box (KUBECONFIG already set by install-k3s.sh):

```bash
cd deploy/sealed-secrets
./install.sh                                   # controller + BACK UP its key offline
kubectl create namespace statusflow
kubectl create namespace cloudflared
cp secrets.env.example secrets.env && $EDITOR secrets.env   # passwords, Stripe/Resend, tunnel creds
./seal.sh                                       # -> sealed/*.yaml (encrypted, commit these)
kubectl apply -f sealed/                        # controller unseals into real Secrets
```

Details + rotation: `deploy/sealed-secrets/README.md`. Apply the sealed secrets
**before** the Helm install — the migration hook needs `statusflow-migrate` to
exist already.

## 4. Get the images onto the node

Build the four images (`Dockerfile.{api,worker,migrate,web}`) and either push to
a registry (set `images.registry` + immutable tags in `values-statusflow.yaml`),
or import tarballs straight into k3s's containerd:

```bash
# on the box, per image:
sudo k3s ctr images import statusflow-api.tar      # etc. for worker/migrate/web
```

Set the tags in `deploy/prod/values-statusflow.yaml` to whatever you built (not
`:dev`).

## 5. Deploy StatusFlow

```bash
helm install pg ./deploy/charts/postgres   -n statusflow -f deploy/prod/values-postgres.yaml
helm install statusflow ./deploy/charts/statusflow -n statusflow -f deploy/prod/values-statusflow.yaml \
  --set ingress.host=statusflow.<domain> \
  --set config.appBaseURL=https://statusflow.<domain> \
  --set web.appOrigin=https://statusflow.<domain>
```

(Or edit `<domain>` in `values-statusflow.yaml` and drop the `--set`s.) The
migration Job (pre-install hook, privileged DSN) runs first; api/worker/web roll
after. Verify:

```bash
kubectl -n statusflow get pods,ingress,netpol
kubectl -n statusflow exec pg-postgres-0 -- \
  psql -U postgres -d statusflow -tAc "select version,dirty from schema_migrations;"   # -> 20|f
```

## 6. Open the edge (Cloudflare Tunnel)

Follow `deploy/cloudflared/README.md`: create the tunnel, route the DNS, fill
`<TUNNEL_ID>`/`<domain>` in the ConfigMap, then:

```bash
kubectl apply -f deploy/cloudflared/namespace.yaml
kubectl apply -f deploy/cloudflared/configmap.yaml
kubectl apply -f deploy/cloudflared/deployment.yaml      # creds already applied in step 3
```

`https://statusflow.<domain>` now serves through Cloudflare → tunnel → Traefik →
web. TLS terminates at Cloudflare's edge (origin Ingress is plain http, by
design). Put `grafana.<domain>` / `argocd.<domain>` (P13/P14) behind **Cloudflare
Access**.

## 7. Smoke test

```bash
curl -s -o /dev/null -w '%{http_code}\n' https://statusflow.<domain>/login   # 200
# Sign up in the browser, create a monitor, confirm the worker records checks.
```

## What's deliberately deferred

- **GitOps (Argo CD)** — P13. For now `kubectl apply` / `helm install` are manual.
- **Observability (Prometheus/Grafana/Loki)** — P14. `/metrics` is already served
  on the ops port for when ServiceMonitors land.
- **Backups** — a `pg_dump` CronJob → object storage + a tested restore is the
  P14/ops follow-up. The Hetzner Volume gives durability across a box rebuild but
  is **not** a backup. Don't let the demo carry data you care about until the
  restore drill is done.
