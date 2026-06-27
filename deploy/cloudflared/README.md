# cloudflared — Cloudflare Tunnel (no open inbound ports)

The cluster has **no public web ports** (ufw denies inbound 80/443). All traffic
arrives through a Cloudflare Tunnel: `cloudflared` runs *inside* the cluster and
dials **out** to Cloudflare, which sends requests back down that connection to
k3s's Traefik. This is the P12 edge.

This is a **locally-managed** tunnel: the routing (`config.yaml`) lives in Git
here, not the Cloudflare dashboard — reviewable and GitOps-ready for P13.

## One-time setup (on your laptop, with the `cloudflared` CLI)

```bash
cloudflared tunnel login                       # browser auth to your CF account
cloudflared tunnel create statusflow           # prints a TUNNEL_ID + writes ~/.cloudflared/<ID>.json
cloudflared tunnel route dns statusflow statusflow.<domain>   # proxied CNAME
# (repeat `route dns` for grafana.<domain>, argocd.<domain> when those exist)
```

Then:

1. Put the printed **TUNNEL_ID** into `configmap.yaml` (replace `<TUNNEL_ID>`)
   and replace `<domain>` with your domain.
2. The `~/.cloudflared/<ID>.json` credentials file is sealed into the
   `cloudflared-credentials` Secret — see `deploy/sealed-secrets/` (it expects
   the file's contents under the key `credentials.json`). **Never commit the raw
   JSON.**

## Apply

```bash
kubectl apply -f deploy/cloudflared/namespace.yaml
kubectl apply -f deploy/sealed-secrets/sealed/cloudflared-credentials.yaml   # sealed creds
kubectl apply -f deploy/cloudflared/configmap.yaml
kubectl apply -f deploy/cloudflared/deployment.yaml
kubectl -n cloudflared rollout status deploy/cloudflared
```

`https://statusflow.<domain>` now resolves through Cloudflare → tunnel → Traefik
→ the StatusFlow web (BFF). Cloudflare terminates TLS at the edge, so the
in-cluster Ingress runs plain http (`ingress.tls.enabled=false` in prod values).

## Gating the ops UIs

Put `grafana.<domain>` and `argocd.<domain>` behind **Cloudflare Access** (Zero
Trust → Access → Applications, email allow-list) so only you reach them, even
though they share the tunnel. The StatusFlow app host stays public.
