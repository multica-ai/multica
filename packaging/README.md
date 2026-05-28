# Multica self-hosting on Kubernetes

This directory contains the assets for running Multica on a Kubernetes cluster:
custom Docker images, a Helm chart, and a build script.

## Plan A (this directory): platform only

Brings up the Multica control plane (Postgres+pgvector, backend, web, ingress).
Agent execution requires either a workstation daemon or the runtime subsystem
(Plan C — see below — or Plan D once it lands).

## Layout

```
docker/postgres/Dockerfile      Custom postgres:17 + pgvector image.
helm/multica/                   The single Helm chart (subsystems gated).
scripts/build-images.sh         Build and push images to a registry.
```

## Operator override values

This repo ships sensible defaults in `helm/multica/values.yaml`. Per-deployment
overrides live OUTSIDE this repo at `~/kube/apps/multica/values.yaml`.

## Build images

```bash
export GHCR_PAT=ghp_…
echo "$GHCR_PAT" | docker login ghcr.io -u <user> --password-stdin
TAG=v0.2.4-mk1 ./scripts/build-images.sh --tag v0.2.4-mk1
```

The build script defaults to `--platform linux/amd64` so images built on Apple
Silicon still run on x86 cluster nodes. Override with `--platform` if needed.

## Install

```bash
helm upgrade --install multica packaging/helm/multica/ \
  --namespace multica --create-namespace \
  -f ~/kube/apps/multica/values.yaml
```

## First-user signup

Without Resend configured, the verification code is in the backend logs:

```bash
kubectl -n multica logs deploy/multica-backend | grep -i 'verification code'
```

## Subsystems

| Values key       | What it deploys                           | Plan       |
|------------------|-------------------------------------------|------------|
| `platform.*`     | postgres, backend, web, ingress           | A (this)   |
| `runtime.*`      | daemon pod (Plan C) or controller (Plan D) | C / D      |
| `bootstrap.*`    | bootstrap Job, token-rotator CronJob, GC  | E          |

Plans C/D/E live in `docs/superpowers/plans/`.

## Runtime subsystem (Plan C — daemon mode)

Runs a Multica agent in-cluster. Build the runtime image, create the worker
secrets, enable `runtime.*` in your override values.

### Build the runtime image

```bash
./scripts/build-images.sh --tag v0.2.4-mk1 runtime
# builds + pushes multica-runtime-base and multica-runtime-claude
```

### Worker secrets (see spec §6.5)

- `multica-token`        — Multica PAT (from web UI Settings → Personal Access Tokens)
- `multica-claude-oauth` — tar of ~/.claude/.credentials.json + settings.json
- `multica-git-ssh`      — ed25519 deploy key for your repos

### Enable

Set `runtime.enabled=true`, `runtime.mode=daemon`, and `runtime.workspaceId`
in `~/kube/apps/multica/values.yaml`, then `helm upgrade --install`.

The daemon authenticates via `MULTICA_TOKEN` env and reaches the backend at the
in-cluster service DNS — it never touches the Cloudflare-Access-gated public host.

### Modes

- `daemon` (this plan): one long-lived daemon pod. Simple, laptop-free.
- `controller` (Plan D): per-task Job pods spawned by a controller. The clean
  target architecture. Same image + secrets; different launch mechanism.
