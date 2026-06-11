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

### Cloudflare / R2 credentials for agents

The runtime image ships `wrangler` (Cloudflare API + R2 bucket ops) and `rclone`
(S3-compatible R2 object sync). Both authenticate purely from env vars, injected
into worker/daemon pods from a K8s Secret — nothing is baked into the image (same
model as `GH_TOKEN`). Off by default; enable under `runtime.cloudflare`:

```yaml
runtime:
  cloudflare:
    enabled: true
    secretName: multica-cloudflare      # K8s Secret the keys come from
    # env maps pod env var → Secret key (defaults shown):
    # CLOUDFLARE_API_TOKEN ← api-token        (wrangler)
    # AWS_ACCESS_KEY_ID    ← access-key-id     (aws CLI + rclone creds)
    # AWS_SECRET_ACCESS_KEY← secret-access-key
    # AWS_ENDPOINT_URL_S3  ← s3-api-endpoint   (aws CLI / SDK v2)
    # RCLONE_S3_ENDPOINT   ← s3-api-endpoint   (rclone; it ignores AWS_ENDPOINT_*)
```

Provide the Secret one of two **mutually exclusive** ways:

1. **Manually** — `kubectl -n multica create secret generic multica-cloudflare
   --from-literal=api-token=… --from-literal=access-key-id=… …`
2. **From Vault via External Secrets Operator** — set
   `runtime.cloudflare.externalSecret.enabled=true` and point `secretStoreRef` /
   `remotePath` at your Vault KV path (default `cloudflare/multica-agent`, keys
   `api-token` / `access-key-id` / `secret-access-key` / `s3-api-endpoint`). The
   chart renders an `ExternalSecret` that materializes `secretName` and keeps it
   synced (ESO must be installed with a SecretStore that can read the path).

Pick one — don't do both. ESO's `creationPolicy: Owner` refuses to adopt a
Secret it didn't create, so enabling the ExternalSecret on top of a
hand-created `multica-cloudflare` errors instead of taking over.

`wrangler r2 bucket create <name>` then works inside any agent task (add a
`CLOUDFLARE_ACCOUNT_ID` entry to `env` if your account has more than one).
`rclone` reaches R2 with `rclone --s3-provider Cloudflare --s3-env-auth lsd
:s3:` — credentials come from `AWS_*` and the endpoint from `RCLONE_S3_ENDPOINT`
(rclone does not honor `AWS_ENDPOINT_URL_S3`).

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

## Claude OAuth broker (Plan F.2)

The `multica-claude-broker` Deployment is the single in-cluster owner of the
Anthropic OAuth refresh state. Worker Job pods don't see the refresh_token
at all — they receive only the current access_token, injected into
`CLAUDE_CODE_OAUTH_TOKEN` via `secretKeyRef` from a Secret the broker
mirrors on every refresh. (claude treats that env var as a static OAuth
bearer; it never tries to refresh on its own, so the rotation race can't
occur.)

This eliminates the concurrent-refresh race that previously corrupted the
shared OAuth grant: multiple worker pods would each rotate the refresh_token
in parallel; whichever lost the race silently wrote an invalid token back
to the Secret; the next reuse hit `invalid_grant` and the cluster went dark.
With the broker, only one process (the leader-elected broker pod) ever
calls `/v1/oauth/token`, and the rotated value lands in a single Secret
that's the canonical source of truth.

### Enabling

```yaml
runtime:
  claudeBroker:
    enabled: true
    image: { name: multica-claude-broker, tag: v0.3.6-mk1 }   # or any tag your registry has
```

`helm upgrade` deploys the broker (Deployment + Service + RBAC +
NetworkPolicy + client ConfigMap) and switches the controller to wire
worker Jobs to use the broker's apiKeyHelper instead of the legacy
`claude-auth` init container.

### Bootstrap (one-time)

The broker reads `{access_token, refresh_token, expires_at}` from a Secret
named `multica-claude-oauth-broker`. The first time you enable it, populate
the Secret from your local Claude Code OAuth grant:

**macOS (Keychain):**

```bash
ACCESS=$(security find-generic-password -s 'Claude Code-credentials' -w \
  | jq -r .claudeAiOauth.accessToken)
REFRESH=$(security find-generic-password -s 'Claude Code-credentials' -w \
  | jq -r .claudeAiOauth.refreshToken)
EXP_MS=$(security find-generic-password -s 'Claude Code-credentials' -w \
  | jq -r .claudeAiOauth.expiresAt)
EXP_AT=$(date -u -r $((EXP_MS / 1000)) +%FT%TZ)

kubectl -n multica create secret generic multica-claude-oauth-broker \
  --from-literal=access_token="$ACCESS" \
  --from-literal=refresh_token="$REFRESH" \
  --from-literal=expires_at="$EXP_AT"
```

**Linux (file-based):**

```bash
ACCESS=$(jq -r .claudeAiOauth.accessToken  ~/.claude/.credentials.json)
REFRESH=$(jq -r .claudeAiOauth.refreshToken ~/.claude/.credentials.json)
EXP_MS=$(jq -r .claudeAiOauth.expiresAt    ~/.claude/.credentials.json)
EXP_AT=$(date -u -d @$((EXP_MS / 1000)) +%FT%TZ)

kubectl -n multica create secret generic multica-claude-oauth-broker \
  --from-literal=access_token="$ACCESS" \
  --from-literal=refresh_token="$REFRESH" \
  --from-literal=expires_at="$EXP_AT"
```

**Important — single-grant caveat:** Your local CLI and the broker now share
the same OAuth grant. The first time the broker refreshes, the rotated
refresh_token is persisted only to the broker's Secret; your local CLI's
keychain entry becomes stale and will need `claude /login` to refresh.
For long-term operation, do `claude /login` on a dedicated machine (or in
a clean profile) and use *that* keychain entry to bootstrap the broker —
your everyday CLI keeps its own grant.

### Force a refresh

The broker's `/refresh` endpoint is bound to `127.0.0.1:8081` inside the pod
(not exposed over the cluster). Operator-only — reachable via `kubectl exec`:

```bash
kubectl -n multica exec deploy/multica-claude-broker -- /bin/sh -c \
  'wget -qO- --post-data= http://127.0.0.1:8081/refresh' || true
```

(The broker image is distroless and has no shell or curl; in practice you
hit the endpoint via `kubectl port-forward` from your workstation:)

```bash
kubectl -n multica port-forward deploy/multica-claude-broker 18081:8081 &
curl -sf -X POST http://127.0.0.1:18081/refresh
```

Each force-refresh consumes one Anthropic rate-limit tick and rotates the
refresh_token; use it for breakglass scenarios (suspected token corruption,
verifying the broker after a config change), not as a routine cron.

### Rotating the OAuth grant

If Anthropic ever revokes the grant, or you want to swap to a fresh login:

1. Run `claude /login` locally (or wherever you keep the operator grant).
2. Re-run the bootstrap commands above to overwrite the Secret.
3. Bounce the broker:

   ```bash
   kubectl -n multica delete pod -l app.kubernetes.io/name=multica-claude-broker
   ```

Total downtime is ~30s. In-flight worker tasks may see one auth failure
during the window; the controller's failure sweep picks them up and the
issue is re-claimable.

### Updating embedded OAuth constants

The broker embeds Claude Code's OAuth client_id, endpoint, and version
header at build time via `go:embed` from
`server/cmd/multica-claude-broker/oauth-constants.json`. The companion
extractor (`server/cmd/extract-oauth-constants/`) regenerates this file
by scanning the Claude Code binary:

```bash
go run ./server/cmd/extract-oauth-constants \
  -binary /path/to/claude.exe \
  -claude-version "$(jq -r .version /path/to/claude-code/package.json)" \
  -out server/cmd/multica-claude-broker/oauth-constants.json
```

Constants drift only when Anthropic rotates the OAuth client_id or
endpoint, which is rare. The watcher described below automates this
via a daily CI job that opens a PR when the extracted JSON changes.

### Claude version watcher

The runtime image's claude version is pinned to a single source of
truth: the plain-text file `packaging/claude-code-version`. The
`Dockerfile.claude` build requires a `CLAUDE_CODE_VERSION` build arg
with no default — an unpinned build fails loudly rather than silently
resolving to `latest`. `packaging/scripts/build-images.sh` reads the
pin file and passes it through automatically.

**Daily auto-bump.** `.github/workflows/claude-version-watch.yml` runs
at 10:00 UTC. It checks `npm view @anthropic-ai/claude-code version`
against the pin file. If they differ, it installs the new claude into
a tmp dir, runs `server/cmd/extract-oauth-constants` against the new
binary, writes the refreshed `oauth-constants.json` plus the new pin,
and opens a PR titled `chore(claude): bump to <version>`. The job is
idempotent — a second run while the PR is open is a no-op.

**Manual bump.** Run `scripts/claude-version-bump.sh` locally. It
performs the same npm + extract + rewrite cycle the workflow does;
inspect the diff and commit it yourself. `--check-only` reports the
delta without mutating files. The script is also what the workflow
shells out to, so any improvement to one improves both.

**Reviewing a watcher PR.** The diff is usually exactly two lines —
the version string and the `_meta.claude_version`/`extracted_at`
fields in `oauth-constants.json`. Patch-level bumps with no semantic
diff in `client_id`, `version_header`, or `scopes` are safe to merge
once CI is green. **Stop and investigate** if any of those three
fields change: Anthropic rotated something, and the broker needs to
keep running against the *previous* values until the new ones are
verified end-to-end. Build a test image with the PR's constants, run
it in a non-prod cluster, and watch
`multica_claude_broker_refresh_failures_total{reason="permanent"}`
stay at zero through at least one full refresh cycle before merging.

**Disabling temporarily.** `gh workflow disable claude-version-watch.yml`
on the repo turns off the daily run without deleting the file. Re-enable
with `gh workflow enable`. To force a one-shot run, use
`gh workflow run claude-version-watch.yml`.

### Metrics + alerting

The broker exposes Prometheus metrics on `:9090`:

```
multica_claude_broker_refresh_total{outcome="ok|error|skipped"}
multica_claude_broker_refresh_failures_total{reason="permanent|transient|not_leader|other"}
multica_claude_broker_refresh_duration_seconds        (histogram)
multica_claude_broker_access_token_expires_at_seconds (gauge — unix time)
multica_claude_broker_access_token_requests_total{outcome="ok|error|stale"}
multica_claude_broker_leader                          (1 if this pod holds the refresh lease)
multica_claude_broker_constants_info{claude_version,extracted_at,version_header}
```

**Recommended alerts:**

| Alert | Expression | Why |
|---|---|---|
| Broker has no leader | `max(multica_claude_broker_leader) == 0 for 5m` | Refresh is blocked; will eventually serve a 503 once cached token expires |
| Permanent refresh failure | `increase(multica_claude_broker_refresh_failures_total{reason="permanent"}[10m]) > 0` | OAuth grant is dead — needs operator intervention (rotate the grant) |
| Token nearing expiry | `multica_claude_broker_access_token_expires_at_seconds - time() < 600` | If this fires for more than a minute, refresh isn't running |

### NetworkPolicy

The chart ships a NetworkPolicy that restricts ingress on the admin port
(`:8080`) to pods labelled
`app.kubernetes.io/managed-by: multica-k8s-controller` — i.e., the worker
Job pods spawned by the controller. The metrics port (`:9090`) is
restricted to whatever scrape source you list in
`runtime.claudeBroker.networkPolicy.metricsFrom`. The ops port (`:8081`)
is bound to loopback inside the pod and isn't reachable from the cluster
network at all.

## Repo cache (Plan F.1)

The `multica-repocache` Deployment maintains bare clones of every
workspace's repos on a single RWX PVC and serves them as a read-only
filesystem to every worker Job pod. The agent's `git clone <origin-url>`
becomes a transparent sub-second `git clone --shared file:///repos/...`
clone via git's `url.<base>.insteadOf` rewrite, mounted at
`/home/multica/.gitconfig` as a per-task ConfigMap. Cold clones drop from
~5–15 s on a typical repo to under a second, and the cluster keeps
running through transient GitHub outages because workers never touch
origin during a task.

### Architecture

- Long-lived Deployment (`replicas: 1`, `strategy: Recreate`) — single
  writer to avoid `git fetch` racing itself on the same bare clone.
- One RWX PVC (`multica-repocache-repos`) at `/repos`. The same PVC is
  mounted **read-only** on every worker Job pod, so multiple concurrent
  workers can clone in parallel without coordinating.
- A 60 s sync loop calls `GET /api/daemon/workspaces/<id>/repos` per
  configured workspace and runs `git fetch` (or `git clone --bare` for
  new repos) on each URL. Errors per workspace are aggregated and don't
  abort the loop.
- The controller reads `repoCache.{enabled,pvcName,mountPath}` from its
  ConfigMap and, when enabled, adds two volumes + two mounts to every
  worker Job spec, plus a per-task `task-<short>-gitconfig` ConfigMap
  with the URL-rewrite block.

### Required storage class

The PVC **must** be backed by an RWX storage class — multiple worker
pods mount it read-only at the same time. NFS-CSI and EFS both work.
ReadWriteOnce will fail to schedule the second pod.

```yaml
runtime:
  repocache:
    enabled: true
    storage:
      storageClass: synology-nfs-csi-rwx    # or equivalent RWX class
      size: 20Gi
```

### Values keys

| Key | Default | Purpose |
|---|---|---|
| `runtime.repocache.enabled` | `true` | Master switch. Disabling falls back to direct origin clones in worker pods (slower, but functional). |
| `runtime.repocache.replicaCount` | `1` | Must stay 1 — see above. |
| `runtime.repocache.image.{name,tag}` | repocache/`""` (→ `image.tag`) | Image override. |
| `runtime.repocache.storage.storageClass` | `""` | **MUST be RWX-capable.** |
| `runtime.repocache.storage.accessMode` | `ReadWriteMany` | |
| `runtime.repocache.storage.size` | `20Gi` | Sized for all mirrored bare clones. |
| `runtime.repocache.fetchInterval` | `60s` | Background refresh tick. |
| `runtime.repocache.resources` | 100m/256Mi req, 1/1Gi limit | Bump for very large org-wide caches. |

### Verifying

```bash
# Pod is healthy
kubectl -n multica get deploy/multica-repocache

# Bare clones landed on the PVC
kubectl -n multica exec deploy/multica-repocache -- ls /repos
kubectl -n multica exec deploy/multica-repocache -- du -sh /repos/* 2>&1 | head -10

# A worker pod sees the cache and gitconfig
POD=$(kubectl -n multica get pods -l app.kubernetes.io/managed-by=multica-k8s-controller \
        --field-selector=status.phase=Running -o name | head -1)
kubectl -n multica exec "$POD" -c runtask -- cat /home/multica/.gitconfig
kubectl -n multica exec "$POD" -c runtask -- ls /repos
```

The gitconfig file should contain blocks like:

```ini
# Fetch: all URL forms redirect to the cache.
[url "file:///repos/<workspace-id>/github.com+chrissnell+graywolf.git"]
        insteadOf = https://github.com/chrissnell/graywolf
        insteadOf = https://github.com/chrissnell/graywolf.git
        insteadOf = git@github.com:chrissnell/graywolf
        insteadOf = git@github.com:chrissnell/graywolf.git

# Push: the same URL forms redirect to the SSH origin so pushes go to
# GitHub directly. Without this, push would route through the insteadOf
# rewrite above to the read-only PVC and fail.
[url "git@github.com:chrissnell/graywolf.git"]
        pushInsteadOf = https://github.com/chrissnell/graywolf
        pushInsteadOf = https://github.com/chrissnell/graywolf.git
        pushInsteadOf = git@github.com:chrissnell/graywolf
```

### How to confirm a clone hit the cache

The behavior of `git`'s `insteadOf` is subtle:

- The agent runs `git clone https://github.com/chrissnell/graywolf`.
- Git resolves the URL through `insteadOf` at fetch time and pulls
  objects from `file:///repos/...`. **No network traffic to github.com.**
- But the URL git **stores** in `<clone>/.git/config` is the *original*
  `https://github.com/chrissnell/graywolf`, not the rewritten form.
- Future fetches/pushes against this remote re-apply `insteadOf` (for
  fetch) or `pushInsteadOf` (for push) at every operation.

So `cat .git/config` will show the original URL. To see what git
actually uses, run:

```bash
git -C <clone> remote get-url origin            # shows rewritten URL
git -C <clone> remote get-url --push origin     # shows push target
```

A clone that hit the cache shows only `Updating files: X% (Y/N)` in
output (no `Receiving objects`, no `Resolving deltas`). A network clone
shows the full receive/resolve/update progress.

### Performance ceiling: workdir storage

The bottleneck for a cached clone is not the network — it's the
file-syscall cost of writing the checked-out working tree to the
per-task PVC (`runtime.controller.workdir.storageClass`). On NFS-backed
storage classes, a 36 MB / 1300-file repo takes ~20 s for the
`Updating files` phase alone. For dramatically faster workers, point
`runtime.controller.workdir.storageClass` at a local-path provisioner
or other node-local storage class — checkout drops to ~1 s for the
same repo, and the per-task PVC has no need to outlive the task.

### Metrics

The repocache exposes Prometheus metrics on `:9090`:

```
multica_repocache_sync_total{workspace_id,outcome="ok|repos_fetch_error|sync_error"}
multica_repocache_fetch_seconds{workspace_id}    (histogram)
```

**Recommended alert:** repeated `sync_error` outcomes for the same
workspace usually mean either a deploy-key permission problem against a
newly added repo, or a transient GitHub outage. The latter is fine;
the former needs operator attention on the `multica-git-ssh` Secret.

### Admin API

The Deployment exposes a small admin API on `:8080`:

- `GET /healthz` → `200 ok`
- `POST /repos/fetch?workspace_id=<ws>&url=<u>` → force a single repo to
  fetch immediately (404 if the URL isn't cached yet)

These are reachable only from inside the namespace; they're meant for
human ops, not for the controller (which doesn't call them).

### Disabling

Set `runtime.repocache.enabled=false` and re-apply. Worker pods will no
longer mount `/repos` or `/home/multica/.gitconfig`; their `git clone`
calls fall back to direct origin URLs over the network. This is a safe
sanity-check path — if a clone behaves unexpectedly, flip the cache off,
re-run the task, and compare.

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
