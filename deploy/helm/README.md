# Multica Helm Chart

A production-ready Helm chart for [Multica](https://multica.ai) — the self-hosted AI agent platform. The chart deploys three components:

- **backend** — Go API server (`ghcr.io/multica-ai/multica-backend`), port 8080
- **frontend** — Next.js web UI (`ghcr.io/multica-ai/multica-web`), port 3000
- **postgresql** — `pgvector/pgvector:pg17` for storage and vector search

## Prerequisites

- Helm 3.x
- kubectl 1.24+
- Kubernetes 1.24+
- A default `StorageClass` in your cluster (or set `backend.storage.storageClass` / `postgresql.storage.storageClass` explicitly)
- [cert-manager](https://cert-manager.io) (optional, for automatic TLS)
- An Ingress controller such as [ingress-nginx](https://kubernetes.github.io/ingress-nginx/) (optional, for external access)

## Quick Start

```bash
# Install with bundled postgres, no ingress — good for local testing
helm install multica ./multica \
  --namespace multica --create-namespace \
  --set config.jwtSecret="$(openssl rand -hex 32)" \
  --set config.appUrl="http://localhost:3000" \
  --set config.frontendOrigin="http://localhost:3000" \
  --set postgresql.auth.password="$(openssl rand -hex 16)"

# Port-forward to test
kubectl -n multica port-forward svc/multica-frontend 3000:3000
# Open http://localhost:3000
```

## Production Deployment

Create a `values-prod.yaml`:

```yaml
global:
  imageTag: "v1.2.3"   # always pin in production

backend:
  replicaCount: 2
  storage:
    size: 50Gi
    storageClass: gp3

frontend:
  replicaCount: 2

postgresql:
  auth:
    postgresPassword: "<generated>"
    password: "<generated>"
  storage:
    size: 100Gi
    storageClass: gp3

ingress:
  enabled: true
  className: nginx
  host: multica.example.com
  tls:
    enabled: true
    secretName: multica-tls

config:
  jwtSecret: "<32-byte hex>"
  appUrl: https://multica.example.com
  frontendOrigin: https://multica.example.com
  allowSignup: "false"
  allowedEmailDomains: example.com
  google:
    clientId: "<id>"
    clientSecret: "<secret>"
    redirectUri: https://multica.example.com/auth/google/callback
  resend:
    apiKey: "<key>"
    fromEmail: noreply@example.com

autoscaling:
  enabled: true
  backend:
    minReplicas: 2
    maxReplicas: 6
  frontend:
    minReplicas: 2
    maxReplicas: 6
```

Install / upgrade:

```bash
helm upgrade --install multica ./multica \
  --namespace multica --create-namespace \
  -f values-prod.yaml
```

## Key Configuration Reference

| Value | Description | Default |
|---|---|---|
| `global.imageTag` | Fallback image tag for all components | `latest` |
| `backend.replicaCount` | Backend replicas | `1` |
| `backend.storage.size` | Size of the uploads PVC | `5Gi` |
| `frontend.replicaCount` | Frontend replicas | `1` |
| `postgresql.enabled` | Deploy bundled pgvector | `true` |
| `postgresql.auth.password` | Application role password | `changeme` |
| `postgresql.storage.size` | Postgres data PVC size | `10Gi` |
| `ingress.enabled` | Create Ingress | `false` |
| `ingress.host` | Public hostname | `""` |
| `ingress.tls.enabled` | Enable TLS on Ingress | `false` |
| `config.jwtSecret` | **Required.** JWT signing key | `""` |
| `config.appUrl` | **Required.** Public URL | `""` |
| `config.frontendOrigin` | CORS/cookie origin | `""` |
| `config.allowSignup` | Allow new signups | `"true"` |
| `config.resend.apiKey` | Transactional email key | `""` |
| `config.google.clientId` | Google OAuth client ID | `""` |
| `config.s3.bucket` | External object storage bucket | `""` |
| `metrics.enabled` | Enable Prometheus endpoint | `false` |
| `autoscaling.enabled` | Enable HPAs | `false` |

See `values.yaml` for the full list.

## Ingress and TLS

This chart ships with an Ingress template tuned for [ingress-nginx](https://kubernetes.github.io/ingress-nginx/). Enable it with:

```yaml
ingress:
  enabled: true
  className: nginx
  host: multica.example.com
  tls:
    enabled: true
    secretName: multica-tls
```

For automatic certificates via cert-manager, add an annotation:

```yaml
ingress:
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
```

cert-manager will create `multica-tls` automatically when the Ingress is applied.

The Ingress routes traffic as follows:

| Path | Destination |
|---|---|
| `/ws` | backend:8080 (WebSocket) |
| `/api` | backend:8080 |
| `/auth` | backend:8080 |
| `/uploads` | backend:8080 |
| `/` (everything else) | frontend:3000 |

## WebSocket Configuration

**Why `/ws` bypasses the frontend:** the Next.js frontend uses build-time `rewrites()` to proxy `/api`, `/auth`, and `/uploads` HTTP traffic to the backend. Those rewrites **do not handle the HTTP Upgrade header**, so WebSocket connections routed through them fail with `400 Bad Request` or hang during the handshake.

The Ingress has to route `/ws` directly to the backend Service, with WebSocket-friendly timeouts. The chart sets these annotations automatically:

```yaml
nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"
nginx.ingress.kubernetes.io/proxy-send-timeout: "3600"
nginx.ingress.kubernetes.io/proxy-http-version: "1.1"
```

ingress-nginx upgrades connections to WebSocket automatically when it sees the `Upgrade: websocket` header from the client, so no extra configuration is required beyond routing `/ws` to the backend.

If you use a different Ingress controller (Traefik, HAProxy, AWS ALB), add the equivalent WebSocket annotations in `ingress.annotations`.

## Using an External PostgreSQL

Disable the bundled pgvector and point the backend at your own Postgres:

```yaml
postgresql:
  enabled: false

config:
  externalDatabaseUrl: "postgres://multica:secret@db.internal:5432/multica?sslmode=require"
  jwtSecret: "..."
  appUrl: "https://multica.example.com"
```

Your database **must** have the `pgvector` extension installed — the backend will fail to start migrations otherwise:

```sql
CREATE EXTENSION IF NOT EXISTS vector;
```

## Upgrading

Always pin image tags in production before upgrading:

```bash
helm upgrade multica ./multica \
  --namespace multica \
  -f values-prod.yaml \
  --set global.imageTag=v1.3.0
```

Backend migrations run on pod startup. The rolling update uses `maxUnavailable: 0` so the old replica keeps serving traffic until the new one passes its readiness probe.

For major version upgrades, review the [release notes](https://github.com/multica-ai/multica/releases) and back up Postgres first:

```bash
kubectl exec -n multica multica-postgres-0 -- pg_dump -U multica multica > backup.sql
```

## Troubleshooting

### WebSocket not connecting

- Confirm the Ingress has `/ws` routed to the backend: `kubectl describe ingress -n multica multica`
- Check the browser console for the WebSocket URL — it should match your `config.appUrl`
- Verify the Ingress controller forwards the `Upgrade` header: `kubectl logs -n ingress-nginx deploy/ingress-nginx-controller`
- If you changed `ingress.className`, add WebSocket annotations appropriate for your controller

### Database connection failed

- Ensure the Postgres pod is `Ready`: `kubectl get pod -n multica -l app.kubernetes.io/component=postgresql`
- Check the backend init container finished waiting: `kubectl logs -n multica <backend-pod> -c wait-for-postgres`
- Validate the secret: `kubectl get secret -n multica multica-secrets -o yaml` and confirm `postgres-user-password` is set
- If using an external DB, confirm `pgvector` is installed and network policy allows traffic from the backend pod

### Pods in CrashLoopBackOff

- `kubectl describe pod -n multica <pod>` — look at events and the last exit code
- Backend missing `JWT_SECRET` or `DATABASE_URL` will crash immediately on startup
- Frontend uses `runAsNonRoot: true` with UID 1000 — make sure your Next.js image allows this (the stock `ghcr.io/multica-ai/multica-web` does)
- Insufficient memory? The backend's in-flight embedding work can spike memory; bump `backend.resources.limits.memory`

### PVC stuck in Pending

- `kubectl describe pvc -n multica` — usually `no storage class` or `waiting for first consumer`
- Set `backend.storage.storageClass` / `postgresql.storage.storageClass` explicitly if your cluster has no default class

## Uninstall

```bash
helm uninstall multica -n multica
```

Helm does **not** delete PVCs by default. To remove persistent data:

```bash
kubectl delete pvc -n multica -l app.kubernetes.io/instance=multica
kubectl delete namespace multica
```
