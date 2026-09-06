# P13 — GitOps with Argo CD

Make **Git the source of truth** for the whole platform. After a one-time
bootstrap, nothing is `kubectl apply`-ed by hand: you edit a manifest, open a PR,
merge it, and Argo CD reconciles the cluster to match — with prune + self-heal so
drift and orphans are corrected automatically, and a one-click rollback to any
previous Git revision.

This builds directly on the P11 charts, P12 VPS/cluster, and the Sealed Secrets
backbone — Argo CD just becomes the thing that *applies* them, declaratively.

```
deploy/argocd/
  install.sh                  # the ONE imperative step: bootstrap Argo CD
  namespace.yaml              # argocd namespace
  config/                     # Argo's own config (applied by install.sh)
    argocd-cmd-params-cm.yaml #   server.insecure (Traefik fronts it, edge TLS at CF)
    ingress.yaml              #   argocd.<domain> -> argocd-server (gate w/ CF Access)
  project.yaml                # AppProject: which repo + namespaces are allowed
  root-app.yaml               # the app-of-apps (the only Application you apply)
  apps/                       # child Applications, ordered by sync-wave
    10-secrets.yaml           #   wave -1: SealedSecret payloads
    20-postgres.yaml          #   wave  0: in-cluster Postgres
    30-statusflow.yaml        #   wave  1: api/worker/web + migration PreSync hook
    40-cloudflared.yaml       #   wave  1: the tunnel edge
```

## How it preserves the StatusFlow invariants (CLAUDE.md §2)

GitOps changes *who applies the manifests*, not the manifests — every isolation
guarantee carries over:

- **Privileged/restricted split.** The migration Job is a Helm
  `pre-install,pre-upgrade` hook; Argo CD maps that to a **PreSync** hook, so it
  runs on the privileged `statusflow-migrate` DSN and must succeed *before*
  api/worker/web roll. The long-running Deployments still mount only the
  `app_user` DSN. Identical behaviour to `helm install`.
- **Secrets stay encrypted in Git.** Argo applies the **SealedSecrets**; only the
  in-cluster controller can decrypt them. Plaintext never touches the repo. The
  derived `Secret`s are owned by the controller, so Argo never prunes or logs
  them.
- **Least-privilege control plane.** The `statusflow` AppProject restricts every
  Application to *this repo* and *these namespaces* — an Application can't be
  pointed at an arbitrary chart or escape into `kube-system`.
- **Worker egress NetworkPolicy, `Secure` cookie, BFF-only ingress** all ship
  unchanged inside the `statusflow` chart.

## Prerequisites (day-0 bootstrap, from P12)

Argo CD reconciles everything *except* the trust anchors that have to exist
before it can. Do these once, by hand (they're in `deploy/vps/README.md` §3):

1. **Sealed Secrets controller** installed and its key backed up offline
   (`deploy/sealed-secrets/install.sh`), and `kubectl apply -f sealed/` done so
   the real Secrets exist. Argo manages the sealed *payloads* afterward; the
   controller stays out of band on purpose (it holds the decryption key).
2. **Namespaces** created: `kubectl create namespace statusflow cloudflared monitoring`
   (the apps use `CreateNamespace=false` — namespaces are a bootstrap concern,
   not app churn). `monitoring` is for P14; `kubectl apply -f
   deploy/observability/namespace.yaml` also creates it.
   The P14 sealed secrets (`grafana-admin`, `alertmanager-discord`,
   `postgres-exporter-dsn`) are produced by `deploy/sealed-secrets/seal.sh` in the
   same `kubectl apply -f sealed/` as the rest.
3. **Images** on the node (registry or `k3s ctr images import`), tags set in
   `deploy/prod/values-statusflow.yaml`.
4. The **repo is pushed** to `https://github.com/<you>/SaaS.git` and reachable by
   Argo (public = nothing to do; private = `argocd repo add` with a PAT).

## Bootstrap

Replace `<you>` (repo owner) in `project.yaml`, `root-app.yaml`, and every
`apps/*.yaml`, and `<domain>` in `config/ingress.yaml`. Then on the box
(KUBECONFIG → k3s):

```bash
cd deploy/argocd
./install.sh                       # installs Argo CD, applies its params + ingress

# hand the platform to Git — applied ONCE, never again:
kubectl apply -f project.yaml
kubectl apply -f root-app.yaml
```

Watch it converge:

```bash
kubectl -n argocd get applications        # root, secrets, postgres, statusflow, cloudflared
# or the UI at https://argocd.<domain> (behind Cloudflare Access)
```

Sync waves serialize the dependency chain: secrets (-1) → Postgres (0) →
statusflow + cloudflared (1) → the P14 observability charts (2) →
observability-config (3, the ServiceMonitor/alerts/dashboard, after the
Prometheus-Operator CRDs from wave 2 exist). The statusflow app retries with
backoff so a cold start (Postgres still electing, secret just unsealed) self-heals.

## The demo: change → PR → merge → auto-sync → rollback

The whole point, and the thing to show in a portfolio walkthrough:

```bash
# 1. Make a change in Git (e.g. bump the API to a new image tag, or scale web)
#    edit deploy/prod/values-statusflow.yaml:  api.tag: v0.13.0
git checkout -b bump-api && git commit -am "deploy: api v0.13.0" && git push
gh pr create --fill && gh pr merge --squash --auto

# 2. Argo CD notices the new commit and auto-syncs (or `argocd app sync statusflow`).
kubectl -n argocd get app statusflow -o wide      # SYNC=Synced, HEALTH=Healthy

# 3. Roll back to any previous Git revision — no kubectl, no chart surgery:
argocd app history statusflow
argocd app rollback statusflow <REVISION>
#    (or just `git revert` the commit and let auto-sync roll it forward)
```

`selfHeal: true` means a manual `kubectl edit`/`scale` is reverted to match Git
within seconds — try it to prove the cluster can't drift from the repo.

## P14 observability (landed)

`apps/50-53` are the multi-source Helm Applications (kube-prometheus-stack, loki,
promtail, prometheus-postgres-exporter) and `apps/54` is the directory app that
applies the StatusFlow ServiceMonitor + PrometheusRule + Grafana dashboard from
`deploy/observability/manifests/`. Values live in Git under
`deploy/observability/values-*.yaml` and are pulled via the multi-source
`$values` ref. See `deploy/observability/README.md` for the metrics/logs/alerts
surface and the Grafana demo. The `grafana.<domain>` host is already wired in the
tunnel — gate it behind Cloudflare Access, same as `argocd.<domain>`.

## Adding a new platform component later

Drop a new `apps/NN-<thing>.yaml` Application, commit, and the root app-of-apps
adopts it on the next sync — no bootstrap, no kubectl. Remote Helm charts use the
multi-source pattern in `apps/50-*.yaml`; add the chart repo to `project.yaml`
`sourceRepos` (and any cluster-scoped kinds it needs to `clusterResourceWhitelist`).
Pre-wire its `<thing>.<domain>` host in `deploy/cloudflared/configmap.yaml` and
gate ops UIs behind Cloudflare Access.

## Notes / honest gaps

- **Not run here** (no box/repo push): like the rest of P12+, this runbook is
  executed against your own k3s + GitHub. The manifests are `kubectl
  apply --dry-run`-shaped and the wave/hook semantics are standard Argo CD.
- `targetRevision: main` tracks the branch tip. For a stricter source of truth,
  pin Applications to a **tag or SHA** and bump it via PR.
- Local admin + Cloudflare Access is the access boundary; wire SSO into the
  AppProject `roles` if you grow past one operator.
