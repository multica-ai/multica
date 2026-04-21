# AgentFarm GitOps Infrastructure

This directory contains Kubernetes manifests for AgentFarm infrastructure components, managed via GitOps (Argo CD + Crossplane).

## Directory Structure

```
gitops/
├── base/                           # Base Kubernetes manifests
│   ├── kustomization.yaml         # Base kustomization configuration
│   ├── deployment.yaml            # Base deployment spec
│   ├── service.yaml               # Base service spec
│   ├── ingress.yaml               # Base ingress rules
│   ├── namespace.yaml             # Namespace definition
│   ├── service-account.yaml       # Service account
│   ├── secret-store.yaml          # External secrets store config
│   └── iam-resources.yaml         # IAM roles and policies (if using Crossplane)
│
└── environments/                   # Environment-specific overlays
    └── tools/                      # Tools environment
        ├── kustomization.yaml     # Tools-specific kustomization
        └── patches/               # Environment-specific patches
            ├── ingress.yaml       # Ingress hostname/TLS patches
            ├── service-account.yaml  # SA annotations (IAM roles)
            ├── iam-role.yaml      # IAM role patches
            └── iam-policy.yaml    # IAM policy patches
```

## Pattern Overview

This structure follows the **base + overlay** pattern recommended by Kustomize:

- **`base/`** contains environment-agnostic Kubernetes manifests — resource definitions that are common across all deployments
- **`environments/tools/`** contains environment-specific patches and overrides — hostname, namespace, IAM roles, image tags, resource limits, etc.

## How It Works

### Base Layer

The `base/` directory contains standard Kubernetes manifests. Each manifest defines a resource (Deployment, Service, Ingress, etc.) in its most generic form. The `base/kustomization.yaml` file lists all resources to include:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - namespace.yaml
  - iam-resources.yaml
  - service-account.yaml
  - secret-store.yaml
  - deployment.yaml
  - service.yaml
  - ingress.yaml
labels:
  - pairs:
      app: agentfarm
    includeSelectors: true
    includeTemplates: true
```

### Environment Overlay

The `environments/tools/` directory patches base resources for the specific environment. The `environments/tools/kustomization.yaml` references the base and applies patches:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: agentfarm
resources:
  - ../../base/
patches:
  - path: patches/ingress.yaml
    target:
      kind: Ingress
      name: agentfarm-ingress
  - path: patches/service-account.yaml
    target:
      kind: ServiceAccount
      name: agentfarm
images:
  - name: agentfarm-image
    newName: ghcr.io/g2crowd/agentfarm-prod:latest
```

Patches override specific fields without duplicating the entire manifest. For example, `patches/ingress.yaml` might set the hostname:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: agentfarm-ingress
spec:
  rules:
    - host: agentfarm.tools.g2crowd.com
├── base/                 # Base manifests for all environments
│   ├── rds-instance.yaml # Crossplane DBInstance for PostgreSQL 17
│   └── kustomization.yaml
└── README.md
```

## Deployment Workflow

### Initial Setup

1. **Create base manifests** in `gitops/base/`:
   - Define Deployment, Service, Ingress, Namespace, ServiceAccount, etc.
   - Add all resources to `base/kustomization.yaml`

2. **Create environment-specific patches** in `gitops/environments/tools/patches/`:
   - Hostname overrides (ingress.yaml)
   - IAM role ARNs (service-account.yaml, iam-role.yaml)
   - Image tags (specified in kustomization.yaml)
   - Resource limits, replicas, etc.

3. **Test locally**:
   ```bash
   kustomize build gitops/environments/tools/
   ```
   This outputs the final manifests with all patches applied. Verify the output before deploying.

### Deployment via PR

**AgentFarm follows a PR-based GitOps workflow.** All changes to manifests go through code review:

1. **Create a feature branch**:
   ```bash
   git checkout -b feature/update-deployment
   ```

2. **Make changes** to base manifests or environment patches

3. **Test locally**:
   ```bash
   kustomize build gitops/environments/tools/ | kubectl apply --dry-run=client -f -
   ```

4. **Commit and open PR**:
   ```bash
   git add gitops/
   git commit -m "Update deployment: add health check endpoint"
   git push origin feature/update-deployment
   ```
   Open PR against `main` branch

5. **Review**: Team reviews manifest changes, checks for security issues, validates configuration

6. **Merge to main**: After approval, merge PR to `main`

7. **ArgoCD syncs automatically**: ArgoCD monitors the `main` branch and applies changes to the cluster when it detects commits to `gitops/environments/tools/`

### Updating Image Tags

To deploy a new version of AgentFarm:

1. Update the image tag in `gitops/environments/tools/kustomization.yaml`:
   ```yaml
   images:
     - name: agentfarm-image
       newName: ghcr.io/g2crowd/agentfarm-prod:abc123def
   ```

2. Open PR, review, merge

3. ArgoCD syncs the new image to the cluster

### Rollback

To rollback a bad deployment:

1. Revert the offending commit:
   ```bash
   git revert <commit-hash>
   git push origin main
   ```

2. ArgoCD syncs the reverted state automatically

Alternatively, use ArgoCD UI to rollback to a previous sync revision.

## Kustomize Best Practices

- **Keep base manifests generic** — no environment-specific values (hostnames, IAM ARNs, image tags)
- **Use patches for overrides** — don't duplicate entire manifests in overlays
- **Strategic merge patches** (default) work for most cases — adds/replaces fields
- **JSON patches** (with `patchesJson6902`) for precise, surgical edits (e.g., removing an array element)
- **Test before merging** — always run `kustomize build` locally to verify output
- **Commit lock behavior** — ArgoCD syncs from a specific commit, so `main` branch is source of truth
- **Label everything** — use `labels` in kustomization.yaml to apply consistent labels across all resources

## Reference

For more on Kustomize patterns and best practices:
- [Kustomize Documentation](https://kubectl.docs.kubernetes.io/guides/introduction/kustomize/)
- [Example: litellm-dashboard](https://github.com/g2crowd/litellm-dashboard/tree/main/gitops)

## Troubleshooting

**Build fails with "resource not found"**:
- Verify all resources listed in `base/kustomization.yaml` exist as files in `base/`

**Patch not applying**:
- Check the `target` selector in `kustomization.yaml` matches the resource `kind`, `name`, and `apiVersion`
- Verify the patch YAML structure matches the target resource structure

**ArgoCD not syncing**:
- Check ArgoCD application points to the correct path (`gitops/environments/tools/`)
- Verify the branch is `main` (ArgoCD tracks `main` by default)
- Check ArgoCD logs for errors

**Namespace issues**:
- Ensure `namespace:` is set in `environments/tools/kustomization.yaml`
- Verify the namespace exists (created by `base/namespace.yaml`)
1. **Create/modify manifests** in this directory
2. **Open PR** against main branch
3. **Merge PR** → Argo CD auto-syncs to cluster
4. **Crossplane provisions** infrastructure in AWS
5. **Verify resources** with kubectl

## Components

### RDS PostgreSQL Instance

**File**: `base/rds-instance.yaml`

Provisions an RDS PostgreSQL 17 instance for AgentFarm persistence.

**Specifications**:
- Instance class: `db.t3.micro`
- Storage: 20GB gp3, encrypted at rest
- Engine: PostgreSQL 17 (pgvector compatible)
- Auto-generated master password (stored in AWS Secrets Manager)
- Backup retention: 7 days

**Prerequisites**:
- Crossplane AWS provider installed in cluster
- `tools-cluster-db-subnet-group` must exist
- Security group must exist and allow PostgreSQL (5432) ingress from tools cluster CIDR
- **pgvector extension availability verified** (run before deployment):
  ```bash
  aws rds describe-db-engine-versions \
    --engine postgres \
    --engine-version 17 \
    --query 'DBEngineVersions[0].SupportedFeatureNames' \
    --output json | grep -i vector
  ```
  If pgvector is not listed, wait for AWS support or use Aurora PostgreSQL

**Post-deployment**:
1. **Wait for RDS to reach 'available' status** (~10-15 minutes):
   ```bash
   kubectl get dbinstance.rds.aws.crossplane.io agentfarm-postgres -n agentfarm \
     -o jsonpath='{.status.atProvider.dbInstanceStatus}'
   # Should output: available
   ```

2. **Get RDS endpoint**:
   ```bash
   ENDPOINT=$(kubectl get dbinstance.rds.aws.crossplane.io agentfarm-postgres -n agentfarm \
     -o jsonpath='{.status.atProvider.endpoint.address}')
   echo $ENDPOINT
   ```

3. **Retrieve master password from AWS Secrets Manager**:
   ```bash
   # Get secret ARN from RDS instance
   SECRET_ARN=$(aws rds describe-db-instances \
     --db-instance-identifier agentfarm-postgres \
     --query 'DBInstances[0].MasterUserSecret.SecretArn' \
     --output text)
   
   # Get password
   PASSWORD=$(aws secretsmanager get-secret-value \
     --secret-id $SECRET_ARN \
     --query SecretString --output text | jq -r .password)
   
   echo $PASSWORD
   ```

4. **Enable pgvector extension** (required for vector similarity search):
   ```bash
   # Connect as master user
   kubectl run -it --rm psql --image=postgres:17 --restart=Never -- \
     psql -h $ENDPOINT -U agentfarm_admin -d agentfarm \
     -c "CREATE EXTENSION IF NOT EXISTS vector;"
   
   # Verify extension is enabled
   kubectl run -it --rm psql --image=postgres:17 --restart=Never -- \
     psql -h $ENDPOINT -U agentfarm_admin -d agentfarm \
     -c "\dx" | grep vector
   ```

5. **Update SSM parameter** (for backend ConfigMap):
   ```bash
   aws ssm put-parameter \
     --name /agentfarm/tools/database_url \
     --value "postgresql://agentfarm_admin:${PASSWORD}@${ENDPOINT}:5432/agentfarm" \
     --type SecureString \
     --overwrite
   ```

## Connection Credentials

The RDS instance exposes credentials in two ways:

1. **Kubernetes Secret** (auto-created by Crossplane):
   ```bash
   kubectl get secret agentfarm-rds-connection -n agentfarm -o yaml
   ```
   Contains keys: `endpoint`, `port`, `username`. Password is **NOT included** — it's managed separately in AWS Secrets Manager for security.

2. **SSM Parameter** (manual setup, step 5 above):
   `/agentfarm/tools/database_url` — full connection string for backend ConfigMap.

**Backend should use SSM parameter** (via ExternalSecret or direct SSM client), not the K8s secret directly, as it includes the password.

## Deletion Notes

**Deletion protection is enabled** (`deletionProtection: true`) to prevent accidental deletion. To delete the RDS instance:

1. **Disable deletion protection first**:
   ```bash
   # Edit manifest: set deletionProtection: false
   # Commit + merge PR → Argo CD syncs → Crossplane updates RDS
   ```

2. **Delete the DBInstance resource**:
   ```bash
   kubectl delete dbinstance.rds.aws.crossplane.io agentfarm-postgres -n agentfarm
   ```

**Snapshot name conflict**: If you delete and recreate the RDS instance, the hardcoded `finalDBSnapshotIdentifier: agentfarm-postgres-final-snapshot` will conflict on the second deletion. Either:
- Delete the snapshot first: `aws rds delete-db-snapshot --db-snapshot-identifier agentfarm-postgres-final-snapshot`
- OR update `finalDBSnapshotIdentifier` in the manifest before recreating

## Local Testing

Validate manifests before committing:

```bash
kustomize build gitops/base
```

## References

- [Crossplane AWS Provider](https://marketplace.upbound.io/providers/upbound/provider-aws)
- [Crossplane DBInstance API](https://marketplace.upbound.io/providers/upbound/provider-aws/latest/resources/rds.aws.upbound.io/Instance/v1beta1)
