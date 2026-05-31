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
- `controller` (Plan E): per-task Job pods spawned by a controller. The clean
  target architecture. Same image + secrets; different launch mechanism.

### `multica run-task` (Plan D primitive)

Single-task execution mode. Reads a JSON Task payload from `--task-file`,
runs the agent end-to-end (start → spawn → stream → complete/fail), exits.
Used by the Plan E controller to drive per-task Job pods. Can also be invoked
manually for smoke tests against a real Multica deployment.

```bash
multica run-task \
  --task-file /etc/task/task.json \
  --workspaces-root /work
```

Required env / flags:

- `MULTICA_TOKEN`            — PAT (same one daemon-mode uses)
- `MULTICA_SERVER_URL`       — backend URL, or `--server-url`
- `MULTICA_RUNTIME_PROVIDER` — provider key (`claude`, `codex`, …). Auto-detected
  when exactly one agent CLI is on PATH (the `multica-runtime-claude` image
  always satisfies that case).

The task payload must include `id`, `workspace_id`, and `runtime_id`. The
controller (or your manual smoke test) is responsible for first claiming the
task via `POST /api/daemon/runtimes/{runtimeID}/tasks/claim` and writing the
returned JSON to `--task-file`. `run-task` itself does not poll or claim.

## Controller mode (Plan E)

Per-task Job pods spawned by the `multica-k8s-controller`. Set
`runtime.mode=controller` in your override values; the chart deploys the
controller Deployment, RBAC, and a ConfigMap with the per-workspace runtime
declarations. The controller polls for tasks every 3s, creates a Job pod per
claim, mounts a per-issue PVC for workdir continuity, and posts `FailTask`
for any Job that dies without the worker reporting.

### Switching from daemon-mode

1. Bump `image.tag` to a tag whose runtime image includes `multica run-task`
   (Plan D).
2. Set `runtime.mode: controller`.
3. Provide a writable storage class for workdir PVCs:
   ```yaml
   runtime:
     controller:
       workdir:
         storageClass: synology-nfs-csi   # any RWO class your cluster offers
         size: 5Gi
   ```
4. `helm upgrade --install`.
5. The daemon Deployment is removed; the controller takes over registration
   under the same device name.

If you had an agent bound to the daemon's runtime row, you'll need a one-time
repoint to the new controller-served row (the daemon and controller use
different `daemon_id`s, so the server treats them as separate runtimes):

```sql
UPDATE agent SET runtime_id = '<new-controller-runtime-id>' WHERE id = '<agent-id>';
```

Inspect with: `SELECT id, name, status, daemon_id FROM agent_runtime WHERE workspace_id = '<ws>';`

### Per-issue PVCs

PVC name: `wd-{ws8}-{ag8}-{scope8}` (RWO). `scope` is the issue short id when
present; for chat tasks it falls back to `c{chat-session-short}`; for
autopilot to `a{run-short}`; otherwise to `t{task-short}` (per-task workdir).
Created lazily on the first task; reused on follow-up tasks; deleted
manually for now (auto-GC on issue close lands in a later plan).

### RBAC

The controller's ServiceAccount has a namespaced Role limited to: Jobs and
PVCs (CRUD), ConfigMaps (CRUD), Pods/Pods-logs (read), Events (create). Scoped
to the install namespace only.

### PodSecurity

Both the controller Deployment and the spawned worker Job pods are compatible
with the `restricted` Pod Security Standard: `runAsNonRoot`, drop ALL caps,
`allowPrivilegeEscalation: false`, and the RuntimeDefault seccomp profile.

### Multica CLI version reporting

The controller embeds `main.version` via `-ldflags` at build time and reports
it as the daemon `cli_version` so Multica's CLI-version gate
(`MIN_QUICK_CREATE_CLI_VERSION` in `server/pkg/agent`) accepts it. If the
binary was built without a real version (the bare `go build` default of
`dev`), the register payload falls back to `v0.3.5` to stay past the gate.

## Local token sync (macOS)

`multica-token-sync` is a tiny launchd agent that follows the cluster-side
Claude OAuth broker as the authoritative writer for the refresh chain. The
broker rotates tokens in the cluster; without this tool, the local Keychain
goes stale every time the broker refreshes, and the local `claude` CLI breaks
until you run `claude /login` again.

The agent polls the broker's state Secret every 30 minutes, transforms the
bytes into the JSON shape Claude Code's macOS Keychain entry expects, and
upserts the entry. The broker becomes the single writer, your laptop is a
read-only follower — `/login` ceremonies disappear after the initial bootstrap.

### Prerequisites

- macOS.
- `kubectl` configured against the cluster with `get` permission on
  `secrets/multica-claude-oauth-broker` in the broker's namespace (default
  `multica`). If `kubectl -n multica get secret multica-claude-oauth-broker`
  works, so will this tool.
- Go 1.26+ to build.

### Install

```bash
cd server && go build -o /tmp/multica-token-sync ./cmd/multica-token-sync
sudo install -m 0755 /tmp/multica-token-sync /usr/local/bin/multica-token-sync
./packaging/launchd/install.sh install
```

The installer copies `com.multica.token-sync.plist` to
`~/Library/LaunchAgents/`, rewrites the `__USER_HOME__` placeholder, and runs
`launchctl bootstrap`. The first sync fires immediately (`RunAtLoad`); the
ticker then runs every 1800s.

### Verify

```bash
./packaging/launchd/install.sh status        # launchd state
tail -f ~/Library/Logs/multica-token-sync.log
```

Expected log on success:
```
INFO msg="keychain updated" service="Claude Code-credentials" account=<you> expires_at=…
```
or
```
INFO msg="keychain already current" fingerprint=…
```

### Manual force-sync

```bash
multica-token-sync --once --verbose
multica-token-sync --dry-run --verbose      # diff without writing
```

### Uninstall

```bash
./packaging/launchd/install.sh uninstall
sudo rm /usr/local/bin/multica-token-sync   # optional
```

### Caveat

A long-running interactive `claude` session holds tokens in memory; a broker
rotation that happens mid-session takes effect at the *next* CLI invocation,
not in-flight. This is the same behavior you'd see if you ran `claude /login`
mid-session — there's nothing the sync tool can do about it.
