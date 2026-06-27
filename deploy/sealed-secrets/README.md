# Sealed Secrets — encrypted secrets in Git

No StatusFlow secret is ever plaintext in the repo. The [Bitnami Sealed
Secrets](https://github.com/bitnami-labs/sealed-secrets) controller holds a
private key in-cluster; you encrypt (`seal`) each Secret against its public cert,
commit the resulting **SealedSecret**, and only the controller can turn it back
into a real Secret. This is the secrets backbone for P12 and the GitOps story in
P13.

## Files

| File | Committed? | What |
|------|-----------|------|
| `install.sh` | yes | install the controller + back up its key |
| `seal.sh` | yes | build all Secrets from `secrets.env` → `sealed/` |
| `secrets.env.example` | yes | template of required plaintext inputs |
| `secrets.env` | **no** (gitignored) | your real passwords/keys |
| `sealed/*.yaml` | yes | the encrypted SealedSecrets (safe in Git) |

## Flow

```bash
export KUBECONFIG=~/.kube/config            # the k3s cluster
./install.sh                                # controller into kube-system
# IMPORTANT: back up the controller key now (printed by install.sh) and store it
# offline — losing it means every sealed secret must be re-sealed after a rebuild.

kubectl create namespace statusflow         # seal targets need to name a namespace
kubectl create namespace cloudflared

cp secrets.env.example secrets.env && $EDITOR secrets.env   # fill passwords/keys
./seal.sh                                   # writes sealed/*.yaml
git add sealed/ && git commit -m "P12: sealed secrets"

kubectl apply -f sealed/                     # controller unseals them into real Secrets
```

The Secret **names** seal.sh emits are exactly what the charts reference via
`existingSecret` (see comments in `seal.sh`), so once these exist the Helm
installs find them. Apply the sealed secrets **before** `helm install` — the
migration Job (a pre-install hook) needs `statusflow-migrate` to already exist.

## Rotation

Re-run `seal.sh` with new values in `secrets.env`, commit, `kubectl apply -f
sealed/`, then restart the consumers (`kubectl -n statusflow rollout restart
deploy`). Rotate anything that was ever in a local `.env` before the repo goes
public.
