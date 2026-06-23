# Multica Helm Chart

Deploys the Multica backend and web services.

## Prerequisites

- Kubernetes cluster + `kubectl` configured
- Helm 3.x
- A reachable PostgreSQL database (its connection string is a required value)

### Create the namespace first

The chart deploys all resources into the `costrict-web` namespace by default
(see `namespace` in `values.yaml`). Because every resource pins
`metadata.namespace`, the namespace **must exist before install** — Helm's
`--create-namespace` only creates the release namespace (`-n`), not the one
pinned by the chart.

Create it once:

```bash
kubectl create namespace costrict-web
```

## Install

```bash
helm install multica ./helm/multica \
  -n costrict-web \
  --set backend.secrets.databaseUrl="postgres://user:pass@host:5432/multica" \
  --set backend.secrets.jwtSecret="<a-strong-random-secret>"
```

The release name (`multica` above) becomes the resource name prefix
(`multica-backend`, `multica-web`, …).

### Namespace behavior

- **Default:** all resources deploy into `costrict-web`. The `helm -n <ns>`
  flag is **ignored** as long as `namespace` keeps its default value.
- **To follow `-n` instead:** clear the chart default so it falls back to the
  release namespace:

  ```bash
  helm install multica ./helm/multica \
    -n my-namespace --create-namespace --set namespace="" \
    --set backend.secrets.databaseUrl="..." \
    --set backend.secrets.jwtSecret="..."
  ```

  With `namespace=""` the `--create-namespace` flag works as expected.

## Service account

Pods run under the namespace's `default` ServiceAccount. The chart does not
create or reference a dedicated ServiceAccount.

## Uninstall

```bash
helm uninstall multica -n costrict-web
```

The uploads PersistentVolumeClaim carries `helm.sh/resource-policy: keep`, so it
**is not deleted** on uninstall — uploaded data is preserved. Delete it manually
if you want to reclaim the storage:

```bash
kubectl delete pvc multica-uploads -n costrict-web
```

> Note: because the PVC is kept, reinstalling under a **different** release name
> can fail with an ownership conflict. Reinstalling under the **same** release
> name and namespace adopts the existing PVC cleanly.
