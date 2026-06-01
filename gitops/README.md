# agentfarm — GitOps

This directory contains the Kubernetes manifests ArgoCD applies for `agentfarm` in the G2 tools cluster. It follows the app-owned GitOps pattern ([ADR-018](https://github.com/g2crowd/architecture-decision-records)).

## Tree

```
gitops/
├── base/
│   ├── namespace.yaml                   # Namespace: agentfarm
│   ├── provider-config.yaml             # Upbound app-scoped ProviderConfig (IRSA) for agentfarm RDS/EC2 resources
│   ├── iam-backend.yaml                 # Crossplane IAM: Role + Policy (S3-only IRSA) + RolePolicyAttachment
│   ├── service-account.yaml             # ServiceAccount: agentfarm (IRSA annotation added by overlay)
│   ├── service-account-backend.yaml     # ServiceAccount: agentfarm-backend (dedicated for S3 access)
│   ├── secret-store.yaml                # ExternalSecret pulling /agentfarm/tools/* from AWS SSM → Secret: agentfarm-secret-store (includes POSTGRES_MASTER_PASSWORD consumed by RDS Cluster)
│   ├── rds-security-group.yaml          # EC2 SG + ingress/egress rules for RDS access from tools VPC
│   ├── rds-subnet-group.yaml            # RDS SubnetGroup reusing tools-cluster VPC subnets
│   ├── rds-cluster.yaml                 # Aurora Serverless v2 Postgres 16.4 cluster
│   ├── rds-instance.yaml                # Aurora ClusterInstance (db.serverless)
│   ├── s3-uploads-bucket.yaml           # S3 bucket g2-agentfarm-tools-uploads for user attachments
│   ├── pgvector-init-job.yaml           # Job (ArgoCD PostSync hook): CREATE EXTENSION vector on first sync
│   ├── deployment-backend.yaml          # Go API server, port 8080, ghcr.io/g2crowd/agentfarm-backend-prod
│   ├── deployment-web.yaml              # Next.js standalone, port 3000, ghcr.io/g2crowd/agentfarm-web-prod
│   ├── service-backend.yaml             # ClusterIP :8080 → backend
│   ├── service-web.yaml                 # ClusterIP :80 → web :3000
│   ├── ingress.yaml                     # nginx-external, agentfarm.g2.com, Cloudflare-proxied
│   └── kustomization.yaml
└── environments/
    └── tools/
        ├── kustomization.yaml           # namespace: agentfarm, image pins (SET_BY_CI)
        └── patches/
            ├── service-account.yaml     # IRSA annotation: agentfarm SA
            └── service-account-backend.yaml # IRSA annotation: agentfarm-backend SA
```

## Architecture at a glance

- **Workloads**: one Deployment for the Go backend (`/health` probe), one for the Next.js frontend (TCP probe on 3000).
- **Traffic**: `agentfarm.g2.com` → NLB → `nginx-external` Ingress → `agentfarm-web` Service → Next.js pod. The frontend proxies `/api/*` to the backend Service.
- **Scheduling**: all pods run on the `shared` Karpenter NodePool targeting arm64 (Graviton), matching vibekanban's pattern. `nodeSelector: {karpenter.sh/nodepool: shared, kubernetes.io/arch: arm64}`. Images are built `linux/arm64` only (on ubicloud `-arm` runners); the `devops` NodePool is amd64-only and would fail with `exec format error`.
- **Secrets**: SSM Parameter Store `/agentfarm/tools/*` → ExternalSecrets Operator (10-minute refresh) → single k8s Secret `agentfarm-secret-store` (bulk `dataFrom.find`, SSM prefix stripped). Reloader rolls pods when SSM values change.
- **IAM**: Two roles via Crossplane. `agentfarm-role` (SSM-read) is assumed by `agentfarm` SA. `agentfarm-backend-role` (S3 bucket operations) is assumed by `agentfarm-backend` SA.
- **Database**: Aurora Serverless v2 Postgres 16.4 provisioned via **upbound Crossplane** (`gitops/base/rds-*.yaml`), following the canonical `optio` and `user-mapping-db` pattern. Master password is seeded into SSM by the operator (single key `POSTGRES_MASTER_PASSWORD`) and read into the RDS Cluster via `masterPasswordSecretRef` pointing at `agentfarm-secret-store`. Upbound then writes `endpoint`, `port`, `master_username`, `attribute.master_password`, `reader_endpoint` back into `agentfarm-rds-connection-secret`. Consumers (backend Deployment, pgvector Job) bind these to `POSTGRES_HOST`/`POSTGRES_PORT`/`POSTGRES_USER`/`POSTGRES_PASSWORD` via `secretKeyRef`, hardcode `POSTGRES_DB=agentfarm` to match `rds-cluster.yaml`'s `databaseName` (Upbound does not echo the db name into the connection secret), and compose `DATABASE_URL` inline with `$(VAR)` substitution — the Go binary reads a single `DATABASE_URL` env var. No secret templating, no bootstrap Job, no manual SSM round-trip. The pgvector init Job runs as an ArgoCD `PostSync` hook.
- **S3 uploads**: `g2-agentfarm-tools-uploads` bucket (us-east-1, AES256 SSE) provisioned via contrib Crossplane. 
- **TLS**: terminates at the NLB via ACM.
- **Image pull**: `imagePullSecrets: [{name: regcred}]` on both Deployments.

## Pre-deploy setup (one-time, required before pods can run)

1. **Populate SSM parameters** under `/agentfarm/tools/` (AWS account `637423279283`, region `us-east-1`). All values are SecureString.

   Required keys:

   | Key (SSM path) | Purpose |
   |---|---|
   | `/agentfarm/tools/POSTGRES_MASTER_PASSWORD` | RDS master password. Seeded pre-deploy. Upbound consumes via `masterPasswordSecretRef`; endpoint/port/username come back in `agentfarm-rds-connection-secret`. |
   | `/agentfarm/tools/JWT_SECRET` | App JWT signing key |
   | `/agentfarm/tools/APP_ENV` | `tools` |
   | `/agentfarm/tools/RESEND_API_KEY` | Resend API key. |
   | `/agentfarm/tools/RESEND_FROM_EMAIL` | Sender From address (default: `noreply@multica.ai`). Domain must be verified in the Resend account. |
   | `/agentfarm/tools/GOOGLE_CLIENT_ID` | OAuth |
   | `/agentfarm/tools/GOOGLE_CLIENT_SECRET` | OAuth |
   | `/agentfarm/tools/GOOGLE_REDIRECT_URI` | `https://agentfarm.g2.com/auth/callback` |
   | `/agentfarm/tools/NEXT_PUBLIC_GOOGLE_CLIENT_ID` | Frontend OAuth client ID |
   | `/agentfarm/tools/MULTICA_SERVER_URL` | `wss://agentfarm.g2.com/ws` |
   | `/agentfarm/tools/MULTICA_APP_URL` | `https://agentfarm.g2.com` |
   | `/agentfarm/tools/S3_BUCKET` | `g2-agentfarm-tools-uploads` |
   | `/agentfarm/tools/S3_REGION` | `us-east-1` |

   DB master user (`agentfarmadmin`) and DB name (`agentfarm`) are hardcoded in `rds-cluster.yaml` — no SSM key required. If you need to change them, edit the manifest (it's a greenfield cluster, not hot-swappable).

   Populate via AWS CLI:
   ```bash
   aws ssm put-parameter --profile tools --region us-east-1 \
     --name /agentfarm/tools/POSTGRES_MASTER_PASSWORD \
     --type SecureString --value "$(openssl rand -base64 32)"
   ```

2. **File PE ticket** for Platform to provision the ArgoCD `AppProject`, `Application`, and repo-credential ExternalSecret in `g2crowd/configuration`.

## CI/CD pipeline

- `.github/workflows/publish.yml` runs on every `push` to `main` (excluding `gitops/**`):
  1. Build backend/web images, push to `ghcr.io/g2crowd/agentfarm-{backend,web}-prod:<sha>`.
  2. Commit new image reference to `gitops/environments/tools/kustomization.yaml`.
  3. ArgoCD picks up the commit within ~3 minutes.
- PR checks: Kustomize Validation, Kustomize Diff (kubechecks), Policy Check (Conftest/OPA), Cost Estimate (Infracost).

## Rollback

Revert the image-tag commit in `gitops/environments/tools/kustomization.yaml`:

```bash
git revert <commit-sha>
git push
```

## Wave 2 / future PRs

### CloudFront signed URLs (next PR)
- `gitops/base/cloudfront-distribution.yaml` — CloudFront Distribution with Origin Access Control to the S3 uploads bucket.
- Extension to `iam-backend.yaml` — add `cloudfront:CreateInvalidation` / `GetDistribution` / `ListDistributions` Statement to the `agentfarm-backend-policy`.
- One-shot manual SE step: generate RSA 2048 keypair offline, upload public key to CloudFront Key Group, put private key + Key Pair ID + distribution domain in SSM under `/agentfarm/tools/CLOUDFRONT_*`.

### Dev / prod cluster rollout
- Currently tools-only. Dev and prod overlays get added later.

### Argo Rollouts / canary
- Eligible once meaningful Prometheus metrics exist.

### Observability
- ServiceMonitor, PrometheusRule, Sloth PrometheusServiceLevel, Grafana dashboard ConfigMaps — eligible once the app has golden signals worth alerting on.

Tracking: PE-865.

## References

- ADR-018 — App-owned GitOps
- `g2crowd/litellm-dashboard` — canonical stateless-app reference
- `g2crowd/gandalf` — canonical publish.yml + gitops-deploy flow
- `g2crowd/configuration/kustomize/Platform/user-mapping-db` — canonical upbound RDS reference
- `g2crowd/configuration/kustomize/Platform/optio` — canonical upbound RDS + deployment consumption reference (the `$(VAR)` inline `DATABASE_URL` pattern used here)

---

# agentrunner — Operator Runbook

Per-workspace cloud runner workload deployed via the `agentrunner-appset` ApplicationSet
(defined in `g2crowd/configuration`). Each workspace gets one pod in its own
`agentrunner-<workspace_id>` namespace on the development cluster.

## Architecture

```
SSM /agentfarm/tools/agentrunner/<workspace_id>   ← enabled flag (value=any)
         │
         │  ESO dataFrom.find (5 min)
         ▼
  Secret: agentrunner-registry (tools cluster, agentrunner-generator ns)
         │
         │  mounted at /run/registry
         ▼
  applicationset-fs-plugin → emits [{slug: "<slug>"}, ...]
         │
         │  plugin generator (ArgoCD, tools cluster)
         ▼
  ApplicationSet agentrunner-appset → one Application per slug
         │
         │  ArgoCD sync (dev cluster)
         ▼
  Namespace: agentrunner-<slug>
    ├── ExternalSecret agentrunner-secrets → Secret (per-ws keys + shared keys fused:
    │     MULTICA_WORKSPACE_ID, LITELLM_API_KEY, MULTICA_PAT, etc.)
    └── Deployment agentrunner (ghcr.io/g2crowd/agentrunner-runtime, single opencode container;
          ATLASSIAN_SITE set as static env var)
```

## SSM Contract

All SSM parameters live in AWS account `637423279283`, region `us-east-1`.

| SSM Path | Type | Writer | Purpose |
|---|---|---|---|
| `/agentfarm/tools/plugin-token` | SecureString | one-shot bootstrap | Plugin bearer token (shared between generator pod and ArgoCD ConfigMap) |
| `/agentfarm/development/agentrunner/shared/MULTICA_PAT` | SecureString | one-shot bootstrap | Bot PAT shared across all workspace runners; swept into `agentrunner-secrets` by the shared `dataFrom.find` entry |
| `/agentfarm/tools/agentrunner/<slug>` | String | gandalf (PLA-383) | Registry entry — existence = workspace enabled |
| `/agentfarm/development/agentrunner/<slug>/MULTICA_WORKSPACE_ID` | SecureString | gandalf (PLA-383) | Workspace UUID — written by gandalf at workspace-create time; flows into the pod via `agentrunner-secrets` ESO + `envFrom` |
| `/agentfarm/development/agentrunner/<slug>/LITELLM_API_KEY` | SecureString | gandalf (PLA-383) | LiteLLM API key for this workspace |
| `/agentfarm/development/agentrunner/<slug>/GIT_USER_EMAIL` | String (optional) | gandalf (PLA-383) | Git user email; triggers acli Jira auth when paired with JIRA_PAT |
| `/agentfarm/development/agentrunner/<slug>/JIRA_PAT` | SecureString (optional) | gandalf (PLA-383) | Jira PAT; triggers acli auth when paired with GIT_USER_EMAIL |

Gandalf SSM automation is tracked in PLA-383 and has landed in [g2crowd/gandalf#138](https://github.com/g2crowd/gandalf/pull/138). For smoke tests, put keys manually.

## Smoke Test Procedure

```bash
SLUG=smoke-test-1
WORKSPACE_UUID=<the-workspace-uuid>
AWS_PROFILE=development
AWS_REGION=us-east-1

# 1. Register the workspace in the registry (triggers ApplicationSet)
aws ssm put-parameter --profile tools --region $AWS_REGION \
  --name "/agentfarm/tools/agentrunner/$SLUG" \
  --type String --value "enabled" --overwrite

# 2. Seed per-workspace secrets (normally written by gandalf at workspace-create time)
aws ssm put-parameter --profile $AWS_PROFILE --region $AWS_REGION \
  --name "/agentfarm/development/agentrunner/$SLUG/MULTICA_WORKSPACE_ID" \
  --type SecureString --value "$WORKSPACE_UUID" --overwrite

aws ssm put-parameter --profile $AWS_PROFILE --region $AWS_REGION \
  --name "/agentfarm/development/agentrunner/$SLUG/LITELLM_API_KEY" \
  --type SecureString --value "<test-key>" --overwrite

# 3. Wait ~2 minutes for ArgoCD to reconcile, then verify
# On tools cluster:
kubectl --context arn:aws:eks:us-east-1:637423279283:cluster/tools -n argocd \
  get application "agentrunner-$SLUG"

# On dev cluster:
kubectl --context arn:aws:eks:us-east-1:975049976121:cluster/development \
  -n "agentrunner-$SLUG" get pods

# 4. Verify MULTICA_WORKSPACE_ID is present in the rendered Secret
kubectl --context arn:aws:eks:us-east-1:975049976121:cluster/development \
  -n "agentrunner-$SLUG" get secret agentrunner-secrets -o jsonpath='{.data.MULTICA_WORKSPACE_ID}' \
  | base64 -d && echo
# Expected: the workspace UUID

# 5. Verify MULTICA_WORKSPACE_ID appears in the running pod's environment
kubectl --context arn:aws:eks:us-east-1:975049976121:cluster/development \
  -n "agentrunner-$SLUG" exec deploy/agentrunner -- printenv MULTICA_WORKSPACE_ID
# Expected: the workspace UUID

# 6. Rollback / deregister
aws ssm delete-parameter --profile tools --region $AWS_REGION \
  --name "/agentfarm/tools/agentrunner/$SLUG"
aws ssm delete-parameters --profile $AWS_PROFILE --region $AWS_REGION \
  --names \
    "/agentfarm/development/agentrunner/$SLUG/MULTICA_WORKSPACE_ID" \
    "/agentfarm/development/agentrunner/$SLUG/LITELLM_API_KEY"
# Application and namespace are pruned by ArgoCD within ~2 minutes.
```

## Troubleshooting

### ApplicationSet stuck / no Application generated

```bash
# Check the generator pod is healthy
kubectl -n agentrunner-generator logs deploy/agentrunner-generator

# Check the registry ESO synced
kubectl -n agentrunner-generator get externalsecret agentrunner-registry
kubectl -n agentrunner-generator get secret agentrunner-registry -o jsonpath='{.data}' | jq 'keys'

# Verify the SSM registry key exists
aws ssm get-parameter --name "/agentfarm/tools/agentrunner/$SLUG"
```

### Pod in CrashLoopBackOff

```bash
kubectl -n "agentrunner-$SLUG" logs deploy/agentrunner --previous

# Common causes:
# - Missing MULTICA_PAT or MULTICA_WORKSPACE_ID: check agentrunner-secrets ESO
#   (MULTICA_WORKSPACE_ID written by gandalf to /agentfarm/development/agentrunner/<slug>/MULTICA_WORKSPACE_ID;
#    MULTICA_PAT from /agentfarm/development/agentrunner/shared/MULTICA_PAT — both fused into agentrunner-secrets)
# - Missing LITELLM_API_KEY: check agentrunner-secrets ESO
#   (written by gandalf to /agentfarm/development/agentrunner/<slug>/*)
```

### ESO not syncing

```bash
# Check the single fused per-workspace ESO
kubectl -n "agentrunner-$SLUG" describe externalsecret agentrunner-secrets

# Verify SSM paths are correct and the runner's IAM role has SSM read access
aws ssm get-parameters-by-path \
  --path "/agentfarm/development/agentrunner/$SLUG/" \
  --with-decryption

aws ssm get-parameters-by-path \
  --path "/agentfarm/development/agentrunner/shared/" \
  --with-decryption
```

## Rollback

To deregister a workspace and destroy its runner:

```bash
# Delete the registry key — ApplicationSet prunes the Application → namespace → pod
aws ssm delete-parameter --profile tools --region us-east-1 \
  --name "/agentfarm/tools/agentrunner/$SLUG"

# Optionally clean up per-workspace SSM secrets
aws ssm delete-parameters --profile development --region us-east-1 \
  --names \
    "/agentfarm/development/agentrunner/$SLUG/MULTICA_WORKSPACE_ID" \
    "/agentfarm/development/agentrunner/$SLUG/LITELLM_API_KEY" \
    "/agentfarm/development/agentrunner/$SLUG/GIT_USER_EMAIL" \
    "/agentfarm/development/agentrunner/$SLUG/JIRA_PAT"
```

To roll back a bad `agentrunner-runtime` image tag:

```bash
# Revert the tag commit in gitops/environments/development/agent-runtime/kustomization.yaml
git revert <commit-sha>
git push
```
