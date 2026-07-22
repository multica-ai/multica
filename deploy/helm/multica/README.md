# Multica Helm chart

This chart deploys Multica into Kubernetes with either the bundled pgvector
PostgreSQL instance or an external PostgreSQL service. It supports external
Redis, SMTP, S3-compatible storage, private registries, custom CAs, restricted
pod security settings, and optional Prometheus Operator resources.

## Requirements

- Kubernetes with `apps/v1`, `networking.k8s.io/v1`, and `policy/v1` APIs.
- Helm 3 or newer.
- A default StorageClass when bundled PostgreSQL or local uploads are enabled.
- A pre-created Secret named by `existingSecret`.
- Prometheus Operator CRDs only when `ServiceMonitor` or `PrometheusRule` is enabled.

The chart creates one ServiceAccount and ConfigMap; backend and frontend
Deployments and Services; two Ingress objects when enabled; and the frontend
compatibility Service named `backend`. It conditionally creates PostgreSQL and
uploads PVCs, the bundled PostgreSQL Deployment and Service, NetworkPolicies,
PodDisruptionBudgets, a metrics Service, ServiceMonitor, and PrometheusRule.

## Secrets

The chart never creates a Secret. Create one before installation and keep the
following values in it when their integrations are enabled:

```text
JWT_SECRET
DATABASE_URL
REDIS_URL
SMTP_USERNAME
SMTP_PASSWORD
AWS_ACCESS_KEY_ID
AWS_SECRET_ACCESS_KEY
AWS_SESSION_TOKEN
RESEND_API_KEY
GOOGLE_CLIENT_SECRET
CLOUDFRONT_PRIVATE_KEY
GITHUB_WEBHOOK_SECRET
GITHUB_APP_PRIVATE_KEY
MULTICA_LLM_API_KEY
```

Bundled PostgreSQL additionally needs `POSTGRES_PASSWORD`. A basic external
production deployment normally needs `JWT_SECRET`, `DATABASE_URL`, `REDIS_URL`,
`SMTP_USERNAME`, `SMTP_PASSWORD`, `AWS_ACCESS_KEY_ID`, and
`AWS_SECRET_ACCESS_KEY`. Empty optional keys do not need to be created.

For example, inject protected GitLab CI variables without storing their values
in the repository:

```bash
kubectl -n multica create secret generic multica-secrets \
  --from-literal=JWT_SECRET='<SET_IN_SECRET>' \
  --from-literal=DATABASE_URL='<SET_IN_SECRET>' \
  --from-literal=REDIS_URL='<SET_IN_SECRET>' \
  --from-literal=SMTP_USERNAME='<SET_IN_SECRET>' \
  --from-literal=SMTP_PASSWORD='<SET_IN_SECRET>' \
  --from-literal=AWS_ACCESS_KEY_ID='<SET_IN_SECRET>' \
  --from-literal=AWS_SECRET_ACCESS_KEY='<SET_IN_SECRET>'
```

The backend loads the chart ConfigMap, `existingSecret`, `backend.extraEnv`,
and `backend.extraEnvFrom` together. Do not repeat `existingSecret` in
`extraEnvFrom`; duplicate references are ignored. The Secret checksum is still
calculated with Helm `lookup` during install and upgrade. Use
`backend.podAnnotations` for a reloader integration when an external controller
must react to out-of-band Secret changes.

## Non-secret backend configuration

`backend.config` maps to the following environment variables:

| Values key | Environment variable |
| --- | --- |
| `appEnv` | `APP_ENV` |
| `appUrl` | `MULTICA_APP_URL` |
| `publicUrl` | `MULTICA_PUBLIC_URL` |
| `frontendOrigin` | `FRONTEND_ORIGIN` |
| `corsAllowedOrigins` | `CORS_ALLOWED_ORIGINS` |
| `allowedOrigins` | `ALLOWED_ORIGINS` |
| `cookieDomain` | `COOKIE_DOMAIN` |
| `databaseMaxConns` | `DATABASE_MAX_CONNS` |
| `databaseMinConns` | `DATABASE_MIN_CONNS` |
| `logLevel` | `LOG_LEVEL` |
| `metricsAddr` | `METRICS_ADDR` |
| `analyticsDisabled` | `ANALYTICS_DISABLED` |
| `shutdownHoldDuration` | `MULTICA_SHUTDOWN_HOLD_DURATION` |
| `resendFromEmail` | `RESEND_FROM_EMAIL` |
| `allowSignup` | `ALLOW_SIGNUP` |
| `allowedEmails` | `ALLOWED_EMAILS` |
| `allowedEmailDomains` | `ALLOWED_EMAIL_DOMAINS` |
| `disableWorkspaceCreation` | `DISABLE_WORKSPACE_CREATION` |
| `googleClientId` | `GOOGLE_CLIENT_ID` |
| `googleRedirectUri` | `GOOGLE_REDIRECT_URI` |
| `smtp.host` | `SMTP_HOST` |
| `smtp.port` | `SMTP_PORT` |
| `smtp.fromEmail` | `SMTP_FROM_EMAIL` |
| `smtp.tls` | `SMTP_TLS` |
| `smtp.tlsInsecure` | `SMTP_TLS_INSECURE` |
| `smtp.ehloName` | `SMTP_EHLO_NAME` |
| `redis.disableClientName` | `REDIS_DISABLE_CLIENT_NAME` |
| `rateLimit.auth` | `RATE_LIMIT_AUTH` |
| `rateLimit.authVerify` | `RATE_LIMIT_AUTH_VERIFY` |
| `rateLimit.trustedProxies` | `RATE_LIMIT_TRUSTED_PROXIES` |
| `trustedProxies` | `MULTICA_TRUSTED_PROXIES` |
| `s3Bucket` | `S3_BUCKET` |
| `s3Region` | `S3_REGION` |
| `s3EndpointUrl` | `AWS_ENDPOINT_URL` |
| `s3UsePathStyle` | `S3_USE_PATH_STYLE` |
| `attachmentDownloadMode` | `ATTACHMENT_DOWNLOAD_MODE` |
| `attachmentDownloadUrlTtl` | `ATTACHMENT_DOWNLOAD_URL_TTL` |
| `cloudfrontDomain` | `CLOUDFRONT_DOMAIN` |
| `cloudfrontKeyPairId` | `CLOUDFRONT_KEY_PAIR_ID` |
| `localUploadBaseUrl` | `LOCAL_UPLOAD_BASE_URL` |

Existing CloudFront and local-upload values remain available in `values.yaml`.
`extraEnv` covers any application variable not modeled directly.

### SMTP

Set `backend.config.smtp.host`, `port`, `fromEmail`, `tls`, `tlsInsecure`, and
`ehloName`. They map to `SMTP_HOST`, `SMTP_PORT`, `SMTP_FROM_EMAIL`, `SMTP_TLS`,
`SMTP_TLS_INSECURE`, and `SMTP_EHLO_NAME`. Keep `SMTP_USERNAME` and
`SMTP_PASSWORD` in `existingSecret` or another Secret referenced by
`extraEnvFrom`.

### Redis and rate limiting

Keep `REDIS_URL` in `existingSecret`. The non-secret options map as follows:

- `backend.config.redis.disableClientName` to `REDIS_DISABLE_CLIENT_NAME`.
- `backend.config.rateLimit.auth` to `RATE_LIMIT_AUTH`.
- `backend.config.rateLimit.authVerify` to `RATE_LIMIT_AUTH_VERIFY`.
- `backend.config.rateLimit.trustedProxies` to `RATE_LIMIT_TRUSTED_PROXIES`.
- `backend.config.trustedProxies` to `MULTICA_TRUSTED_PROXIES`.

### S3-compatible storage

Set `s3Bucket`, `s3Region`, `s3EndpointUrl`, optional `s3UsePathStyle`,
`attachmentDownloadMode`, and `attachmentDownloadUrlTtl`. Credentials and an
optional session token belong in a Secret. Disable the local uploads claim when
S3 is authoritative:

```yaml
backend:
  uploads:
    persistence:
      enabled: false
```

## External PostgreSQL

Create `DATABASE_URL` in `existingSecret`, then set:

```yaml
postgres:
  external:
    enabled: true
```

This removes the bundled PostgreSQL Deployment, Service, and PVC. Database
migrations still run from the backend image during startup. Size the external
pool for `backend.replicas * databaseMaxConns` plus operational headroom.

## Private registry and immutable images

Set `imagePullSecrets` once; it is applied to backend, frontend, and bundled
PostgreSQL. Each image accepts `repository`, `tag`, and `digest`. A non-empty
digest produces `repository@digest`; otherwise the chart uses `repository:tag`.
Empty backend and frontend tags retain the `Chart.appVersion` fallback.

## Corporate CA and pod security

Mount an existing ConfigMap or Secret through `extraVolumes` and
`extraVolumeMounts`; the chart does not include CA material. Backend, frontend,
and PostgreSQL have empty pod and container security contexts by default. A
production values file can enable `runAsNonRoot`, `RuntimeDefault` seccomp,
disabled privilege escalation, and dropped capabilities. Read-only root filesystems
are not enabled by default because application write paths have not been fully
verified.

All workloads use the shared ServiceAccount. With `serviceAccount.create=false`,
`serviceAccount.name` is required and must identify an existing account.

## Availability, shutdown, and networking

Backend startup, readiness, and liveness probes are fully replaceable through
values. Startup and liveness use `/health`; readiness uses `/readyz`, which also
checks PostgreSQL and required migrations. `backend.lifecycle` accepts an
arbitrary lifecycle, including `preStop`. Keep `MULTICA_SHUTDOWN_HOLD_DURATION` below
`terminationGracePeriodSeconds` and leave time for shutdown work after the hold.

NetworkPolicy and PDB resources are disabled by default. When NetworkPolicy is
enabled, the chart passes component ingress and egress arrays through unchanged;
empty arrays isolate that direction. Add explicit rules for DNS, PostgreSQL,
Redis, S3, SMTP, and other enabled integrations, restricted with selectors or
CIDRs appropriate to the cluster.

Common Ingress annotations apply to both Ingress objects. Component annotations
are merged over them and let operators configure backend WebSocket and long
connections without coupling the chart to an ingress controller.

## Monitoring

Set `backend.metrics.enabled=true` and bind `backend.config.metricsAddr` to the
configured container port, for example `0.0.0.0:9090`. This adds a metrics port
and Service. A ServiceMonitor is created only when metrics and
`monitoring.serviceMonitor.enabled` are both true. PrometheusRule remains
independently optional.

## Install and upgrade

Create an environment-specific `values-production.yaml` outside the chart,
create referenced Secrets, ConfigMaps, and registry credentials, then install:

```bash
helm upgrade --install multica ./deploy/helm/multica \
  --namespace multica --create-namespace \
  --values values-production.yaml \
  --wait --atomic
```

Upgrade with the same release, namespace, and values file, changing only an
immutable tag or digest:

```bash
helm upgrade multica ./deploy/helm/multica \
  --namespace multica \
  --values values-production.yaml \
  --set-string images.backend.tag=v0.3.6 \
  --set-string images.frontend.tag=v0.3.6 \
  --wait --atomic
```

The default frontend image requires the compatibility Service named `backend`.
Because that name is not release-prefixed, install only one chart release per
namespace. Set `frontend.compatibility.backendAlias=false` only for a frontend
image built with a different backend URL.

For workspace bootstrap, first deploy with
`backend.config.disableWorkspaceCreation=false`, sign in and create the main
workspace, then upgrade with it set to `true`. Keep signup enabled or use the
email allowlist until all invited users have accounts.

Generic OIDC or Authentik deployment is outside this chart production change.
The chart does not install an identity provider or add authentication support;
pass related variables through `extraEnv` or `extraEnvFrom` only when the
application version already supports generic OIDC.
