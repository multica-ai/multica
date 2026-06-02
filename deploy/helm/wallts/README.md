# Wallts Helm Chart

Self-hosted deployment of Wallts on Kubernetes.

## Quick Start

```bash
# Install with bundled PostgreSQL (default)
helm install wallts ./deploy/helm/wallts

# Install with external PostgreSQL
helm install wallts ./deploy/helm/wallts \
  --set postgresql.enabled=false \
  --set postgresql.external.databaseUrl="postgres://user:password@my-db-host:5432/wallts?sslmode=require"
```

## PostgreSQL Configuration

### Bundled PostgreSQL (default)

By default the chart deploys a pgvector-enabled PostgreSQL instance as part of
the release. The database credentials are pulled from the pre-created
`wallts-secrets` Secret (`POSTGRES_PASSWORD` key).

### External PostgreSQL

To disable the bundled instance and point at an external database:

```bash
helm install wallts ./deploy/helm/wallts \
  --set postgresql.enabled=false \
  --set postgresql.external.databaseUrl="postgres://user:password@my-db-host:5432/wallts?sslmode=require"
```

Or in a custom values file:

```yaml
postgresql:
  enabled: false
  external:
    databaseUrl: "postgres://user:***@my-db-host:5432/wallts?sslmode=require"
```

When `postgresql.enabled=false`:

- No PVC, Deployment, or Service is created for PostgreSQL.
- The backend uses `postgresql.external.databaseUrl` as `DATABASE_URL`.
- The `POSTGRES_DB` and `POSTGRES_USER` ConfigMap entries are omitted (no
  longer needed).
- The `POSTGRES_PASSWORD` key in `existingSecret` is unused — credentials
  must be embedded in the connection URL or managed externally.

### Validation

Dry-run the chart to verify it renders correctly with your values:

```bash
helm template wallts ./deploy/helm/wallts \
  --set postgresql.enabled=false \
  --set postgresql.external.databaseUrl="postgres://user:password@db:5432/wallts?sslmode=disable" \
  | kubectl apply --dry-run=client -f -
```
