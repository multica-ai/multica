# Cerebro Mini Apps runtime

This service is the private control plane and frontend origin for published Mini Apps. The Cerebro server remains the source of truth for immutable bundles, grants, active versions, and deployment state. Each backend bundle runs in its own private Sliplane service with QuickJS memory and execution limits; the control plane has no Docker socket.

## Required production configuration

The Cerebro server and runtime must share `CEREBRO_APPS_RUNTIME_SERVICE_KEY`. Set `CEREBRO_APPS_RUNTIME_URL` on the server to the runtime's private URL and configure the runtime with:

- `CEREBRO_APPS_BACKEND_URL`: private Cerebro server URL
- `CEREBRO_APPS_RUNTIME_PROVIDER=sliplane`
- `CEREBRO_APPS_SLIPLANE_PROJECT_ID`: project that owns app services
- `CEREBRO_APPS_SLIPLANE_SERVER_ID`: the same server as the control plane
- `CEREBRO_APPS_WORKER_BRANCH`: the explicit Git branch used to build private workers
- `CEREBRO_APPS_WORKER_COMMIT`: the exact 40-character commit that the worker health check must report before a deployment becomes ready
- `SLIPLANE_KEY`: service-management credential

Published app services are created with `network.public=false`. The runtime stores the concrete `.internal` domain returned by Sliplane and never guesses a service hostname.

Deployment remains `provisioning` until a private health check to the returned `.internal` worker domain succeeds. A timeout records a failed deployment and never switches the app's current version. Rollback follows the same provision, private health check, callback, and version-switch sequence.

## Publish and rollback

An app bundle contains `app.json` plus files below `frontend/` and `backend/`. Publish from a local directory:

```sh
multica app publish APP_ID --dir ./my-app --version 1.2.0 --release-notes "Add order approval"
```

Publishing validates every path, file hash, manifest entrypoint, and size limit before storing the immutable version. The previous version stays active while the new private service is provisioning. A version with scopes becomes active only after both deployment readiness and scope approval.

```sh
multica app rollback APP_ID --version 1.1.0
multica app disable APP_ID
```

Rollback only accepts a ready deployment. Disable pauses the current private service. App deletion removes every version service before deleting the database record; a provider failure leaves the app record intact for retry.

## Recovery

On restart, the control plane asks Cerebro for all `pending` and `provisioning` deployments and resumes them idempotently. Sliplane create conflicts are resolved by looking up the deterministic service name on the configured server. Bundle and callback requests use short-lived, app-version-bound tokens or signed service requests with a two-minute replay window.
