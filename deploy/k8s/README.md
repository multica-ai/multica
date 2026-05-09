# Kubernetes deployment (ACK)

Deploys Multica to Alibaba Cloud Container Service for Kubernetes (ACK) behind
ALB Ingress. Database is expected to be a managed RDS PostgreSQL instance — no
StatefulSet is shipped here.

```
deploy/k8s/
├── base/                    ← shared resources
│   ├── namespace.yaml
│   ├── configmap.yaml       ← non-secret env
│   ├── secret.yaml          ← placeholder; DO NOT commit real values
│   ├── migrate-job.yaml     ← runs ./migrate up once per rollout
│   ├── server-deployment.yaml + service.yaml
│   ├── web-deployment.yaml + service.yaml
│   ├── ingress.yaml         ← ALB Ingress for ship.lilithgames.com
│   └── kustomization.yaml
└── overlays/
    └── prod/                ← production overlay (image tag, replicas)
        └── kustomization.yaml
```

## One-time cluster setup

1. **Install ALB Ingress Controller** in the ACK console (`Operations → Add-ons`).
   The controller reconciles the `AlbConfig` CRD and creates the ALB itself.
2. **Upload the TLS cert** for `ship.lilithgames.com` to Aliyun SSL Certificate
   Service and copy its id.
3. **Edit `base/albconfig.yaml`**:
   - Paste two `vSwitchId` values in different AZs under `zoneMappings` —
     `tofu output -json vswitch_zones` lists id → zone for you.
   - Paste the SSL cert id under `listeners[].certificates[].CertificateId`.
4. **DNS** — after `kubectl apply`, read the ALB address from the Ingress
   status and CNAME `ship.lilithgames.com` at it:
   ```
   kubectl -n multica get ingress multica \
     -o jsonpath='{.status.loadBalancer.ingress[0].hostname}'
   ```

## Create the Secret (don't commit!)

The Secret in `base/secret.yaml` is a *template* with placeholder values. Before
the first deploy, replace it with real values via one of:

### Option A — create once from CLI (simplest)

```bash
kubectl create namespace multica
kubectl create secret generic multica-secrets --namespace=multica \
  --from-literal=JWT_SECRET="$(openssl rand -hex 32)" \
  --from-literal=DATABASE_URL='postgres://USER:PASS@HOST:5432/multica?sslmode=require' \
  --from-literal=FEISHU_APP_SECRET='...' \
  --from-literal=RESEND_API_KEY='...'
```

Then remove `secret.yaml` from `base/kustomization.yaml`'s `resources:` list
(or patch it empty via an overlay) so deploys don't overwrite the live Secret
with placeholders.

### Option B — fill in `secret.yaml` locally and `kubectl apply -f`

Never commit the filled file. Add it to `.gitignore` if you keep it on disk.

### Option C — External Secrets Operator + Aliyun KMS

Preferred once you have the operator installed. Replace `secret.yaml` with an
`ExternalSecret` manifest that fetches from a KMS-backed SecretStore. Outside
the scope of this PR.

## Deploy

```bash
# First-time dry-run to see everything that would be applied
kustomize build deploy/k8s/overlays/prod | less

# Apply (namespace + config + secret template + workloads + ingress + job)
kubectl apply -k deploy/k8s/overlays/prod

# Wait for migrate Job to finish (~5-30s typical)
kubectl -n multica wait --for=condition=complete job/multica-migrate --timeout=5m

# Check rollout
kubectl -n multica rollout status deployment/multica-server
kubectl -n multica rollout status deployment/multica-web

# Grab the ALB address once the Ingress is bound
kubectl -n multica get ingress multica
```

## Rollout a new image

```bash
TAG=v0.2.1
cd deploy/k8s/overlays/prod
kustomize edit set image \
  lilith-registry.cn-shanghai.cr.aliyuncs.com/devops/multica-server=lilith-registry.cn-shanghai.cr.aliyuncs.com/devops/multica-server:$TAG \
  lilith-registry.cn-shanghai.cr.aliyuncs.com/devops/multica-web=lilith-registry.cn-shanghai.cr.aliyuncs.com/devops/multica-web:$TAG
kubectl apply -k .
kubectl -n multica delete job multica-migrate --ignore-not-found  # force fresh run
kubectl apply -k .                                                 # recreates job
kubectl -n multica wait --for=condition=complete job/multica-migrate --timeout=5m
kubectl -n multica rollout status deployment/multica-server
kubectl -n multica rollout status deployment/multica-web
```

## Troubleshooting

```bash
# Logs
kubectl -n multica logs -l app.kubernetes.io/component=server --tail=200 -f
kubectl -n multica logs -l app.kubernetes.io/component=web    --tail=200 -f
kubectl -n multica logs job/multica-migrate

# Exec into a server pod
kubectl -n multica exec -it deploy/multica-server -- sh

# Inspect effective env (redacts the Secret values by default)
kubectl -n multica describe deploy multica-server
```

If the ALB Ingress has no address after a few minutes, check the controller
logs:

```bash
kubectl -n kube-system logs deploy/alb-ingress-controller --tail=200
```

## Image builds

The existing root Dockerfiles build both images:

```bash
docker build -f Dockerfile     -t lilith-registry.cn-shanghai.cr.aliyuncs.com/devops/multica-server:$TAG .
docker build -f Dockerfile.web -t lilith-registry.cn-shanghai.cr.aliyuncs.com/devops/multica-web:$TAG    .
docker push lilith-registry.cn-shanghai.cr.aliyuncs.com/devops/multica-server:$TAG
docker push lilith-registry.cn-shanghai.cr.aliyuncs.com/devops/multica-web:$TAG
```

ACK worker nodes in the same Aliyun account as the ACR Enterprise Edition
instance auto-authenticate via RAM role — no `imagePullSecret` required.

## What's NOT in here (by design)

- **Database**: use Aliyun RDS PostgreSQL and point `DATABASE_URL` at it.
- **Daemon**: runs on-prem (the daemon dials out to the server — no in-cluster
  Deployment needed). See `SELF_HOSTING_ADVANCED.md`.
- **Object storage**: if you want S3/OSS uploads, fill the S3_* keys in the
  Secret; otherwise the backend falls back to local disk (not recommended in
  a multi-replica setup — pods don't share disk).
