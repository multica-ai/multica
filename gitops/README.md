# AgentFarm GitOps Infrastructure

This directory contains Kubernetes manifests for AgentFarm infrastructure components, managed via GitOps (Argo CD + Crossplane).

## Directory Structure

```
gitops/
├── base/                 # Base manifests for all environments
│   ├── rds-instance.yaml # Crossplane DBInstance for PostgreSQL 17
│   └── kustomization.yaml
└── README.md
```

## Deployment Workflow

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
