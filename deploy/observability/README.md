# P14 — Observability

Metrics, logs, and alerting for the StatusFlow platform, delivered through the
same Argo CD app-of-apps as everything else. The meta-hook: an uptime product
that monitors its own uptime.

## What runs

| Component | Chart | Purpose |
|-----------|-------|---------|
| **kube-prometheus-stack** | `prometheus-community/kube-prometheus-stack` | Prometheus, Alertmanager, Grafana, node-exporter, kube-state-metrics |
| **Loki** (single-binary) | `grafana/loki` | Log store (filesystem, one node) |
| **promtail** | `grafana/promtail` | Ships pod logs → Loki |
| **postgres-exporter** | `prometheus-community/prometheus-postgres-exporter` | Basic Postgres metrics (`pg_up`, connections) — connects as `app_user`, no privileged role |

All four are Argo CD **multi-source** Applications (`deploy/argocd/apps/50-53`):
the pinned chart from its Helm repo + this repo (`ref: values`) so the
`values-*.yaml` here stay in Git. `apps/54` is a directory app that applies the
StatusFlow-specific config from `manifests/` (wave 3, after the Prometheus-Operator
CRDs from wave 2).

## What's scraped / dashboarded / alerted

- **Scrape.** `manifests/servicemonitor.yaml` scrapes the api + worker `/metrics`
  on their `ops` port (`jobLabel` → `job=api`/`job=worker`). The app already
  exposed 9 `statusflow_*` metrics; P14 added
  `statusflow_worker_incidents_total{action}` and `statusflow_worker_ssrf_blocked_total`.
- **Dashboards.** `manifests/dashboard-statusflow.yaml` (a ConfigMap the Grafana
  sidecar auto-imports) — request rate/latency/errors, monitor check outcomes +
  latency, incidents, SSRF blocks, auth-throttle hits, claim batch size,
  invitations. **Cluster / node / kube-state / Postgres-pod** views come free from
  the dashboards bundled with kube-prometheus-stack — not re-authored here.
- **Logs.** api/worker log slog JSON; promtail ships it to Loki with
  namespace/pod/app labels. The api now enriches each request line with
  `org_id`/`user_id` (`internal/http/middleware`), so a line is filterable to a
  tenant in Grafana → Explore → Loki.
- **Alerts.** `manifests/alerts.yaml` → **Discord** (Alertmanager reads the
  webhook from the sealed `alertmanager-discord` secret): `StatusFlowApiDown`,
  `StatusFlowWorkerDown`, `PostgresDown`, `HighCheckFailureRate`, `SSRFBlockSpike`,
  `AuthThrottleSpike` — on top of the many kube-prometheus-stack defaults
  (crashloop, TargetDown, node health, …). *Cert-expiry alerting is intentionally
  omitted: edge TLS is Cloudflare's and the in-cluster Ingress is plain http.*

## Secrets (Sealed Secrets, `monitoring` ns)

Produced by `deploy/sealed-secrets/seal.sh` from `secrets.env`; nothing sensitive
is inline in these values:

- `grafana-admin` — Grafana admin login (`grafana.admin.existingSecret`).
- `alertmanager-discord` — the Discord webhook, mounted at
  `/etc/alertmanager/secrets/alertmanager-discord/webhook_url` and read via
  `webhook_url_file`.
- `postgres-exporter-dsn` — the `app_user` DSN for the exporter.

## Security / invariants

- Prometheus scrapes over the api/worker **ops** port only, which the chart
  NetworkPolicy now pins to this namespace
  (`networkPolicy.monitoringNamespace: monitoring` in
  `deploy/prod/values-statusflow.yaml`) — no other pod can reach it.
- The postgres-exporter connects as **`app_user`** (NOSUPERUSER/NOBYPASSRLS) —
  invariant §2 holds, no new/privileged DB role.
- The AppProject (`deploy/argocd/project.yaml`) allow-lists exactly the
  cluster-scoped kinds kube-prometheus-stack needs (CRDs, cluster RBAC, admission
  webhooks) and the two chart repos — least-privilege GitOps intact.

## The demo

1. `grafana.<domain>` (behind Cloudflare Access) → **StatusFlow — Application**
   dashboard + the bundled cluster dashboards.
2. Grafana → Explore → **Loki**: `{namespace="statusflow"} | json | org_id="…"`.
3. Scale the api to 0 (or `git revert` a deploy) → watch `StatusFlowApiDown` fire
   to Discord, then recover.

## Render-verify (no box here)

Like P12/P13, these are authored and `helm template` / `yq`-verified, not applied
to a live cluster (the repo still carries `<you>`/`<domain>` placeholders):

```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo add grafana https://grafana.github.io/helm-charts && helm repo update
helm template kube-prometheus-stack prometheus-community/kube-prometheus-stack \
  --version 87.19.0 -n monitoring -f deploy/observability/values-kube-prometheus-stack.yaml
# …and loki 7.1.0, promtail 6.17.1, prometheus-postgres-exporter 8.2.0
```

> `grafana/promtail` is in maintenance (Grafana points new setups at Alloy). It's
> kept here for a simple, well-understood single-node log path; swapping to Alloy
> is a values-only change later.
