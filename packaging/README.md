# Multica self-hosting on Kubernetes

This directory contains the assets for running Multica on a Kubernetes cluster:
custom Docker images, a Helm chart, and a build script.

## Plan A (this directory): platform only

Brings up the Multica control plane (Postgres+pgvector, backend, web, ingress).
Agent execution requires either a workstation daemon or Plans C/D (runtime
subsystem) — not in scope for this directory yet.

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
| `runtime.*`      | controller, repo-cache, RBAC              | D          |
| `bootstrap.*`    | bootstrap Job, token-rotator CronJob, GC  | E          |

Plans C/D/E live in `docs/superpowers/plans/`.
