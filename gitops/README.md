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

**Post-deployment**:
1. Get RDS endpoint:
   ```bash
   kubectl get dbinstance.rds.aws.crossplane.io agentfarm-postgres -n agentfarm \
     -o jsonpath='{.status.atProvider.endpoint.address}'
   ```

2. Retrieve master password from AWS Secrets Manager:
   ```bash
   aws secretsmanager get-secret-value \
     --secret-id <secret-arn-from-rds-console> \
     --query SecretString --output text | jq -r .password
   ```

3. Update SSM parameter:
   ```bash
   aws ssm put-parameter \
     --name /agentfarm/tools/database_url \
     --value "postgresql://agentfarm_admin:<password>@<endpoint>:5432/agentfarm" \
     --type SecureString \
     --overwrite
   ```

## Local Testing

Validate manifests before committing:

```bash
kustomize build gitops/base
```

## References

- [Crossplane AWS Provider](https://marketplace.upbound.io/providers/upbound/provider-aws)
- [Crossplane DBInstance API](https://marketplace.upbound.io/providers/upbound/provider-aws/latest/resources/rds.aws.upbound.io/Instance/v1beta1)
