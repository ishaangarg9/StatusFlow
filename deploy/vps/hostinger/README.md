# StatusFlow on the shared Hostinger VPS (runbook)

The live deployment: **https://statusflow.ishaangarg9.com**, with
`statusflow.usefulmachines.tech` as a 301 to it. Single-node **k3s** + **Argo CD**
on a Hostinger KVM (Ubuntu 26.04, 2 vCPU / 8 GiB / 100 GB) that it **shares**
with an existing Docker Compose stack (`/opt/useful-machines`: Caddy, n8n,
n8n's Postgres).

This is a variant of the Hetzner/Cloudflare-Tunnel runbook in `../README.md`.
What differs, and why:

| | Hetzner runbook (P12) | This box |
|---|---|---|
| Edge | Cloudflare Tunnel, zero inbound web ports | The box's existing **Caddy** (Docker) already owns 80/443; it terminates TLS and proxies to Traefik |
| Second domain | — | `usefulmachines.tech` DNS is at Hostinger (no tunnel possible); Caddy 301s it to the canonical host |
| Host hardening | `harden.sh` (new `deploy` user, ufw denies 80/443) | Already hardened; `harden.sh` would break the existing sites. `prep-host.sh` adds only what k3s needs |
| DB disk | Separate Hetzner Volume | Root disk, so a nightly `pg_dump` is on from day one |
| Argo apps | All (10–54) | App tier only (10/20/30) — observability doesn't fit next to n8n in 8 GiB |
| Images | Unspecified | GHCR, built by `.github/workflows/images.yml` after CI passes |

The CLAUDE.md §2 split is unchanged: the migration Job (PreSync) and the backup
CronJob are the only workloads with the privileged credential, and both are
short-lived; api, worker and the demo seed Job connect only as `app_user`.

```
internet ──443──> Caddy (Docker, auto-HTTPS)
                    │  statusflow.usefulmachines.tech ──301──> statusflow.ishaangarg9.com
                    ▼
                 172.17.0.1:8080  (Traefik hostPort on the docker0 bridge: private)
                    ▼
                 Traefik ──> web (Next.js BFF) ──> api ──> pg-postgres
                                                  worker ─┘ (+ SSRF-guarded egress)
```

## 0. One-time accounts

- **GitHub**: nothing to do. After the first `Images` run, open each of the four
  `statusflow-*` packages (github.com/ishaangarg9?tab=packages) → *Package
  settings* → **Change visibility → Public**. GHCR creates packages private even
  for a public repo, and k3s pulls without credentials.
- **DNS**:
  - Cloudflare (`ishaangarg9.com`): `A statusflow → 201.18.199.154`, **DNS only**
    (grey cloud). If you later turn the proxy on, set SSL/TLS to **Full
    (strict)** first, or Caddy and Cloudflare redirect-loop.
  - Hostinger (`usefulmachines.tech`): `A statusflow → 201.18.199.154`.
- **Email (Resend)**, optional. Without it invites land in the worker's outbox:
  1. Create a Resend account and **add the domain `mail.ishaangarg9.com`**. A
     subdomain keeps StatusFlow's sending reputation off the apex and can't
     disturb any mail already received on `ishaangarg9.com`.
  2. Add the records Resend shows in Cloudflare (typically `MX send.mail`,
     `TXT send.mail` (SPF), `TXT resend._domainkey.mail` (DKIM)). Add
     `TXT _dmarc` → `v=DMARC1; p=none;` only if no DMARC record exists.
  3. **Verify**, then create an API key with **Sending access** scoped to that
     domain. It goes into `secrets.env` as `RESEND_API_KEY` (step 3); the From
     address (`noreply@mail.ishaangarg9.com`) is already in the prod values.

## 1. Prep the host + install k3s

```bash
scp -r deploy/vps/hostinger root@201.18.199.154:/root/statusflow-host
ssh root@201.18.199.154
cd /root/statusflow-host
bash prep-host.sh        # swap, sysctl, ufw route rules for pods, backup dir
bash install-k3s.sh      # k3s, servicelb off, Traefik on 172.17.0.1:8080 only
```

From your laptop, prove Traefik is **not** public (must time out):

```bash
curl -m 5 http://201.18.199.154:8080/
```

## 2. Sealed Secrets controller + namespaces

```bash
git clone https://github.com/ishaangarg9/StatusFlow.git /root/StatusFlow
cd /root/StatusFlow/deploy/sealed-secrets
./install.sh
# Back up the controller key OFF the box (losing it = re-seal everything):
kubectl -n kube-system get secret -l sealedsecrets.bitnami.com/sealed-secrets-key -o yaml > /root/sealed-secrets-key.backup.yaml
#   then from the laptop:  scp root@201.18.199.154:/root/sealed-secrets-key.backup.yaml ~/safe/place/
kubectl create namespace statusflow
```

`seal.sh` needs `kubeseal` on the box: download the matching
`kubeseal-<version>-linux-amd64.tar.gz` from the sealed-secrets release page and
put the binary in `/usr/local/bin`.

## 3. Seal the secrets (and commit only the encrypted output)

```bash
cp secrets.env.example secrets.env && chmod 600 secrets.env
# Fill: POSTGRES_SUPERUSER_PASSWORD, APP_USER_PASSWORD (openssl rand -base64 24),
#       RESEND_API_KEY (or leave empty). Leave STRIPE_* and TUNNEL_* empty.
#       Set WITH_OBSERVABILITY=0.
$EDITOR secrets.env
./seal.sh
kubectl apply -f sealed/
```

Commit `deploy/sealed-secrets/sealed/*.yaml` (from the box, or `scp` them back
to the laptop) and push. `secrets.env` is gitignored; delete it from the box
once sealed, or keep it only in a password manager.

## 4. Images

Merging to `main` runs CI; when it passes, `Images` pushes
`ghcr.io/ishaangarg9/statusflow-{api,worker,web,migrate}:sha-<12>` and commits
the tag bump into `deploy/prod/values-statusflow.yaml`. Wait for that bump
commit (and make the packages public, step 0) before step 5. The values file
ships with `tag: pending-ci` until then.

## 5. Argo CD + hand over to Git

```bash
cd /root/StatusFlow/deploy/argocd
./install.sh                        # pinned v3.5.x, trimmed (no dex/notifications/appset)
kubectl apply -f project.yaml
kubectl apply -f root-app.yaml      # syncs: secrets (-1) -> postgres (0) -> statusflow (1)
kubectl -n argocd get applications -w
```

Argo CD's UI stays private. To open it:

```bash
ssh -L 8080:localhost:8080 root@201.18.199.154 \
  'kubectl -n argocd port-forward svc/argocd-server 8080:80'
# browse http://localhost:8080  (admin / argocd-initial-admin-secret)
```

## 6. Open the edge (Caddy)

```bash
cd /opt/useful-machines
cp Caddyfile Caddyfile.bak-$(date +%F)
cat /root/StatusFlow/deploy/caddy/statusflow.caddy >> Caddyfile
docker compose exec caddy caddy validate --config /etc/caddy/Caddyfile
docker compose exec caddy caddy reload --config /etc/caddy/Caddyfile
```

Caddy fetches the two certificates on first request (DNS from step 0 must
already resolve). Confirm the existing sites still answer:

```bash
curl -sI https://usefulmachines.tech | head -1
curl -sI https://n8n.usefulmachines.tech | head -1
```

## 7. Smoke test

```bash
curl -s -o /dev/null -w '%{http_code}\n' https://statusflow.ishaangarg9.com/login     # 200
curl -sI https://statusflow.usefulmachines.tech/x | grep -iE '^(HTTP|location)'      # 308/301 -> canonical
kubectl -n statusflow get pods,jobs,cronjobs
```

In the browser: **Live demo** (viewer-only; write controls hidden and the API
403s), then sign up, create a monitor, and watch checks arrive. Invite a second
address to test email (Resend) or check the outbox (no Resend).

## 8. Backups + restore drill

Nightly at 03:30 UTC to `/opt/statusflow/backups` (14 days kept). Run one now
and prove it restores:

```bash
kubectl -n statusflow create job --from=cronjob/pg-postgres-backup backup-now
kubectl -n statusflow logs -f job/backup-now
ls -lh /opt/statusflow/backups
# restore into a scratch DB inside the pod:
f=$(ls -t /opt/statusflow/backups/*.dump | head -1)
kubectl -n statusflow exec -i pg-postgres-0 -- sh -c 'createdb -U postgres restore_check && pg_restore -U postgres -d restore_check' < "$f"
kubectl -n statusflow exec pg-postgres-0 -- psql -U postgres -d restore_check -tAc 'select count(*) from organizations'
kubectl -n statusflow exec pg-postgres-0 -- dropdb -U postgres restore_check
```

These dumps are on the same disk as the database: they cover a bad migration or
a dropped table, not a dead disk or a deleted VPS. Copying them off-box (R2/B2
via `rclone` in a host cron) is the follow-up.

## Memory budget (approx.)

| | RSS |
|---|---|
| Existing: n8n + its Postgres + Caddy + dockerd | ~1.0 GiB |
| k3s (server, containerd, coredns, traefik, metrics-server, sealed-secrets) | ~0.7 GiB |
| Argo CD trimmed (server, repo-server, application-controller, redis) | ~0.6–0.8 GiB |
| StatusFlow (postgres, api, worker, web) | ~0.6–1.0 GiB |
| **Headroom** of 7.7 GiB (+4 GiB swap) | **~4 GiB** |

## Known trade-offs

- **Origin IP is public** (it already was, via Caddy). No WAF/DDoS layer in front
  of `statusflow.usefulmachines.tech`; `ishaangarg9.com` can gain one by turning
  on the Cloudflare proxy (Full (strict)).
- **Traefik is bound to docker0's IP** (`172.17.0.1`). If Docker's default
  bridge address ever changes, re-run `install-k3s.sh` with `TRAEFIK_BIND_IP`
  and update `deploy/caddy/statusflow.caddy`.
- **Billing is inert** (no Stripe keys) and `/api/stripe/webhook` is off the
  Ingress. To enable: seal the `STRIPE_*` keys and set
  `ingress.exposeApiWebhook: true`.
