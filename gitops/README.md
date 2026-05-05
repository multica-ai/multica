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
│   ├── runtime-controller/              # Per-workspace daemon runtime — see docs/proposals/per-workspace-daemon-runtimes.md
│   │   ├── serviceaccount.yaml          # ServiceAccount: agentfarm-runtime-controller
│   │   ├── rbac.yaml                    # Namespace-scoped Role(s) + RoleBindings (no cluster-scope perms)
│   │   ├── iam.yaml                     # Crossplane IAM: ssm:GetParameter on /agentfarm/tools/runtime-controller/* + tag-scoped ec2:DeleteVolume (no iam:*)
│   │   ├── workspaceiam-xrd.yaml        # Crossplane CompositeResourceDefinition: WorkspaceIAM
│   │   ├── workspaceiam-composition.yaml # Crossplane Composition: per-workspace Role + Policy + RolePolicyAttachment
│   │   ├── storageclass.yaml            # gp3 encrypted, reclaimPolicy: Retain, EBS volumes tagged for controller cleanup
│   │   ├── configmap.yaml               # Cloud-tuned daemon defaults (MAX_CONCURRENT_TASKS=2, GC_TTL=6h)
│   │   ├── externalsecret-controller.yaml # Pulls LiteLLM Management Key + proxy URL from /agentfarm/tools/runtime-controller/*
│   │   ├── deployment.yaml              # Controller Deployment (1 replica, --dry-run=true by default for v1)
│   │   ├── templates/                   # NOT applied — per-workspace shapes the controller renders at runtime
│   │   │   ├── README.md                # What's in here and why it's not in kustomization.yaml resources
│   │   │   └── externalsecret-per-workspace.yaml
│   │   └── kustomization.yaml
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
- **IAM**: Three roles via Crossplane. `agentfarm-role` (SSM-read) is assumed by `agentfarm` SA. `agentfarm-backend-role` (S3 bucket operations) is assumed by `agentfarm-backend` SA. `agentfarm-runtime-controller-role` (SSM read on its own prefix + tag-scoped `ec2:DeleteVolume`) is assumed by `agentfarm-runtime-controller` SA. The runtime controller deliberately has **no** `iam:*` permissions; per-workspace IAM roles are reconciled by Crossplane from `WorkspaceIAM` CRs the controller writes (see `runtime-controller/workspaceiam-composition.yaml`).
- **Runtime controller**: `agentfarm-runtime-controller` (single replica, leader-elected) reconciles per-workspace daemon runtimes — Deployment, ServiceAccount, PVC, ExternalSecret, `WorkspaceIAM` CR, plus LiteLLM team + virtual keys per workspace. Per-workspace resources are intentionally **not** in Git. v1 ships with `--dry-run=true`; flip to write mode after the smoke test in [the proposal's rollout step 3](../docs/proposals/per-workspace-daemon-runtimes.md#13-rollout).
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
   | `/agentfarm/tools/MULTICA_SERVER_URL` | `https://agentfarm.g2.com` — read by the `multica` CLI / daemon. The daemon's `NormalizeServerBaseURL` (`server/internal/daemon/config.go`) accepts `ws://`, `wss://`, `http://`, `https://` interchangeably and strips a trailing `/ws`, but standardize on the HTTPS form here so the value matches what the runtime controller injects into per-workspace Secrets. |
   | `/agentfarm/tools/MULTICA_APP_URL` | `https://agentfarm.g2.com` |
   | `/agentfarm/tools/S3_BUCKET` | `g2-agentfarm-tools-uploads` |
   | `/agentfarm/tools/S3_REGION` | `us-east-1` |
   | `/agentfarm/tools/runtime-controller/LITELLM_MANAGEMENT_KEY` | LiteLLM **Management** key (NOT master). Permissions: `team/{new,update,delete}`, `key/{generate,update,delete}` (only on teams it created), spend reads. Used by `agentfarm-runtime-controller` to mint per-workspace virtual keys. See [proposal §7](../docs/proposals/per-workspace-daemon-runtimes.md#7-controller-scoped-litellm-credentials). |
   | `/agentfarm/tools/runtime-controller/LITELLM_PROXY_URL` | URL of the LiteLLM proxy the controller calls (`/team/new`, `/key/generate`, etc.) and that becomes `ANTHROPIC_BASE_URL` / `OPENAI_BASE_URL` in per-workspace daemon Secrets. |

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
