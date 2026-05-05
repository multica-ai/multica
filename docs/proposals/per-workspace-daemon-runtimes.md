# Per-workspace daemon runtimes on Kubernetes

Status: Draft (rev 3 — incorporates PR #66 review feedback)
Tracking issue: TCA-7

## Why

Today the `multica` daemon runs on engineers' laptops. It detects local CLIs
(`claude`, `codex`, `cursor-agent`, …), registers each as a runtime against a
watched workspace, polls the server for claimed tasks, and executes them in
`~/multica_workspaces/`. That works for individual users but doesn't give us a
managed, always-on runtime that a workspace can rely on without anyone's
laptop being awake.

We want to host daemons in EKS, **one logical runtime per agentfarm
workspace**, so a workspace's agents can pick up tasks 24/7 without a
human-owned machine. Per-workspace isolation is non-negotiable: a daemon's
filesystem, IAM identity, secrets, and PVC must not be shared across tenants.

## High-level shape

A small `agentfarm-runtime-controller` deployed via GitOps. It runs a
standard Kubernetes reconciliation loop — observes desired state from the
agentfarm server (workspaces with `runtime_enabled`), compares against
observed state in the cluster, and converges. The exact reconciliation
semantics for v1 are spelled out under "v1 reconciliation behavior" below
(short version: existence checks and recreate-if-missing, not full
field-level drift repair).

It reconciles a per-workspace runtime spec into per-workspace Kubernetes
resources:

```
GitOps (this repo, gitops/base/)              Controller-managed (cluster runtime)
─────────────────────────────────────         ─────────────────────────────────────
agentfarm-runtime-controller Deployment   ──► Deployment   agentfarm-daemon-<wsId>
ServiceAccount + Role/Binding             ──► ServiceAccount agentfarm-daemon-<wsId>
Dockerfile.daemon + docker-bake.daemon    ──► PVC          agentfarm-daemon-<wsId>-workdir
Crossplane Composition (per-ws IAM shape) ──► WorkspaceIAM CR  ──► (Crossplane provider creates IAM Role)
ExternalSecret template                   ──► ExternalSecret agentfarm-daemon-<wsId>-secrets
StorageClass (gp3, encrypted, retain)     ──► (uses StorageClass)
ConfigMap: shared daemon config           ──► (mounted by all daemon pods)
Controller ExternalSecret (LiteLLM admin) ──► LiteLLM team + virtual keys
                                          ──► Daemon-scoped MULTICA_TOKEN (via server API)
```

The controller never calls AWS IAM directly. It writes a `WorkspaceIAM`
Composition CR per workspace; the existing Crossplane provider pod (which
already has IAM permissions in this cluster) reconciles the CR into the
real role. This keeps `iam:*` out of the controller's IRSA grant entirely
— see §2 and §4.

Per-workspace resources are intentionally **not** in Git: there can be
hundreds, they churn (workspace add/delete), and committing them would force
the controller to PR into this repo, which is the wrong direction.

**v1 reconciliation behavior — explicit existence checks only.** For v1
the controller's reconcile loop is intentionally narrow: for each
workspace returned by the discovery endpoint, check that each expected
resource (Deployment, SA, PVC, Secret, ExternalSecret, `WorkspaceIAM`
CR, LiteLLM team, LiteLLM keys) **exists**. If a resource is missing,
recreate it. If a resource exists, leave it alone — even if its spec has
drifted from the controller's template.

This means out-of-band drift is **not** repaired in v1:

- An operator who hand-edits a per-workspace Deployment (e.g. bumps
  resources for a heavy workspace) keeps their change until the
  Deployment is deleted.
- A `WorkspaceIAM` Composition that was edited in the AWS console will
  not be re-templated; Crossplane will fight back to its CR spec, which
  is what we want anyway.
- LiteLLM team budget / key model lists changed in the LiteLLM dashboard
  stay changed — the controller does not GET-then-PATCH on every
  reconcile.

This is a deliberate scope cut. Full drift detection and repair (diff
the live spec against the template, re-apply when they diverge) is a
v2 problem — it raises hard questions (which side wins on conflict?
how do we surface drift to operators?) that don't need to be answered
to ship v1. Recreate-if-missing is enough to handle the failure modes
we actually care about: a pod was deleted, a node failed, a Secret was
GC'd. See open question §13.6.

## In scope for GitOps

### 1. Controller Deployment

- New manifest `gitops/base/runtime-controller/deployment.yaml`. Single
  replica with leader election (controller-runtime), arm64 /
  `karpenter.sh/nodepool: shared`, same security-context posture as
  `agentfarm-backend` (non-root, read-only rootfs, drop ALL caps).
- Image: `ghcr.io/g2crowd/agentfarm-runtime-controller-prod`, tag pinned in
  `gitops/environments/tools/kustomization.yaml` and bumped by the publish
  workflow on `main` (same pattern as backend/web).
- Env: `MULTICA_SERVER_URL`, `WORKSPACE_POLL_INTERVAL`, `DAEMON_IMAGE`,
  `DAEMON_IMAGE_TAG`, `IRSA_OIDC_PROVIDER_ARN`, `LITELLM_PROXY_URL`. The
  daemon image tag is templated into every per-workspace Deployment the
  controller creates, so bumping the daemon image is a one-line change to the
  controller's env in `kustomization.yaml`.

### 2. RBAC and ServiceAccount

- `ServiceAccount: agentfarm-runtime-controller`.
- A namespace-scoped `Role` in `agentfarm` granting CRUD on `Deployments`,
  `Pods`, `Secrets`, `PVCs`, `ServiceAccounts`, `ExternalSecrets`, and the
  Crossplane `WorkspaceIAM` Composition CR (see §4). No cluster-scope perms
  — the controller only manipulates resources inside `agentfarm`.
- A second `Role` for status/log reads (`pods/log`, `events`) used by the
  health surface the controller exposes back to the server.
- IRSA: the controller's AWS `Role` is **deliberately narrow** — no `iam:*`
  permissions. IAM-role lifecycle is delegated to Crossplane (see §4), so
  the only AWS perms the controller needs are:
  - `ssm:GetParameter` on `/agentfarm/tools/runtime-controller/*` so it can
    read its LiteLLM master key (see §7).
  - `ec2:DeleteVolume` on volumes tagged
    `kubernetes.io/cluster/<cluster>=owned` and
    `agentfarm.workspace=<wsId>`, used during purge to reclaim the orphaned
    EBS volume left behind by the `Retain` StorageClass (see §10).

  Dropping `iam:*` from the controller's IRSA was a direct response to a
  review concern: a compromised controller pod with create-role/put-policy
  perms — even path-scoped — is a privilege bomb; pushing role lifecycle
  through Crossplane means we never grant those perms to the controller in
  the first place.

### 3. `Dockerfile.daemon` and image pipeline

- New `Dockerfile.daemon` at repo root. Reuses the existing
  `golang:1.26-alpine` builder stage (cached deps), then a runtime stage that
  installs the supported agent CLIs we want available server-side.
  **v1 set: `claude` (Claude Code) and `codex` (OpenAI Codex)**. Both
  versions are pinned so the image is reproducible. Other CLIs added on
  demand as workspaces ask for them.
- Dedicated `docker-bake.daemon.hcl` — **not** folded into
  `docker-bake.backend.hcl`. The daemon and backend have independent
  release cadences (a CLI version bump shouldn't rebuild the backend, and
  vice versa); coupling them in the same bake file would churn the publish
  workflow. The new bake file reuses the same builder/cache pattern the
  backend uses.
- `.github/workflows/publish.yml` gets a separate matrix entry for the
  daemon, producing `ghcr.io/g2crowd/agentfarm-daemon-prod:<sha>` on every
  push to `main`. Built `linux/arm64` only, matching backend/web.
- Daemon entrypoint: `/app/multica daemon start --foreground`. The pod
  authenticates via the `MULTICA_TOKEN` env var the Go CLI already honors
  (see `server/cmd/multica/cmd_auth.go`'s `resolveToken`), so there's no
  `multica login` / OAuth step inside the container — see §8 for how the
  controller mints and injects this token.

### 4. IAM Composition (Crossplane) and ExternalSecret template

The controller does not call AWS IAM directly — it writes a Crossplane CR
and lets Crossplane's provider pod (which already runs in this cluster
with IAM perms) do the AWS-side work. This is the same pattern
`iam-resources.yaml` and `iam-backend.yaml` already use; the new piece is
making the per-workspace shape into a reusable `Composition` instead of
hand-edited manifests.

- `gitops/base/runtime-controller/composition-workspace-iam.yaml` — a
  Crossplane `Composition` defining `WorkspaceIAM` as a composite that
  bundles a `Role` + `Policy` + `RolePolicyAttachment`. Per-workspace fields
  (`workspaceID`) are exposed as `CompositeResourceDefinition` parameters.
  Each instance produces:
  - `s3:GetObject/PutObject` on
    `g2-agentfarm-tools-uploads/workspaces/<wsId>/*` only.
  - `ssm:GetParameter` on `/agentfarm/tools/daemons/<wsId>/*` only.
  - Trust policy bound to
    `system:serviceaccount:agentfarm:agentfarm-daemon-<wsId>` via the
    existing OIDC provider.
- The controller creates one `WorkspaceIAM` CR per workspace at runtime
  (a plain Kubernetes write — the controller's k8s `Role` covers it via
  the CR's API group). Crossplane reconciles the CR into the AWS resources.
  Deleting the CR triggers Crossplane to delete the AWS role and policy.
- `gitops/base/runtime-controller/externalsecret-template.yaml` — template
  the controller stamps into a per-workspace `ExternalSecret` that pulls
  `/agentfarm/tools/daemons/<wsId>/*` from SSM into Secret
  `agentfarm-daemon-<wsId>-secrets`. Same Reloader annotation as backend so
  secret changes roll the daemon pod.

### 5. PVC StorageClass

- `gitops/base/runtime-controller/storageclass-daemon.yaml` — dedicated
  `StorageClass: agentfarm-daemon-workdir`, `gp3` encrypted,
  `reclaimPolicy: Retain`, `volumeBindingMode: WaitForFirstConsumer`. Retain
  is deliberate: a daemon PVC holds `~/multica_workspaces/` (cloned repos,
  in-progress work). Accidental delete of the per-workspace Deployment must
  not lose the workdir; explicit GC path handles deletion (see §10).

### 6. Shared static config

`gitops/base/runtime-controller/configmap-daemon-defaults.yaml` — mounted
into every daemon pod. Holds non-sensitive defaults that are tuned for the
cloud-runtime profile, not the laptop defaults baked into the daemon
binary:

| Key                                     | Value | Why it differs from the binary default |
|-----------------------------------------|-------|----------------------------------------|
| `MULTICA_DAEMON_MAX_CONCURRENT_TASKS`   | `2`   | Binary default is 20, sized for a laptop. With 512Mi requests / 1Gi limits and two CLI subprocesses (claude + codex) each consuming ~200–400 MiB under load, 20 will OOM-kill the pod and requeue every in-flight task as `runtime_recovery`. 2 gives clean backpressure: the daemon stops claiming new work instead of crashing. We'll raise this as the smoke test (rollout step 1) measures actual consumption. |
| `MULTICA_GC_TTL`                        | `6h`  | Binary default is 24h. On a 10 GiB PVC shared across all tasks for a workspace, a busy workspace would fill the volume between GC sweeps. 6h leaves comfortable headroom while still letting an agent come back to a recent workdir within a working day. |
| `MULTICA_GC_ARTIFACT_TTL`               | `2h`  | Binary default is 12h. Build artifacts (`node_modules`, `.next`, `.turbo`) regenerate fast; aggressive cleanup keeps disk usage flat. |
| `MULTICA_DAEMON_POLL_INTERVAL`          | `3s`  | Same as binary default. |
| `MULTICA_DAEMON_HEARTBEAT_INTERVAL`     | `15s` | Same as binary default. |
| `MULTICA_AGENT_TIMEOUT`                 | `2h`  | Same as binary default. |

Per-workspace overrides (if we ever need them) live in the per-workspace
Secret instead, so this ConfigMap is truly shared. The concurrency and GC
values may move into per-workspace settings later (open question §13.2);
for v1 they're cluster-wide and decided.

### 7. Controller-scoped LiteLLM credentials

The controller does **not** use the LiteLLM master key. Instead a
dedicated **management-scoped** virtual key is minted once (out-of-band,
by an operator) on the LiteLLM proxy with just the permissions the
controller needs:

- Team management: `team/new`, `team/update`, `team/delete`.
- Key management: `key/generate`, `key/update`, `key/delete` (only
  scoped under teams the management key created).
- Spend / budget read for diagnostics.

Anything the controller doesn't need (model upserts, user CRUD, proxy
config) is excluded. If the management key is leaked, blast radius is
"agentfarm teams + the keys we ourselves minted" — not the entire
LiteLLM proxy.

- `gitops/base/runtime-controller/externalsecret-controller.yaml` — pulls
  `LITELLM_MANAGEMENT_KEY` and `LITELLM_PROXY_URL` from
  `/agentfarm/tools/runtime-controller/*` into a controller-only Secret.
  Neither value ever enters a per-workspace Secret. The controller's
  IRSA policy reads this SSM prefix; per-workspace IAM has no access.
- Rotation runbook: when we rotate the management key, mint a new one,
  update the SSM parameter, Reloader rolls the controller pod onto the
  new credential, then the old key gets `key/delete`'d. The key has no
  expiry-at-issue (LiteLLM doesn't auto-expire admin keys), so rotation
  is human-driven on a quarterly cadence — same as our other long-lived
  service credentials.

## Out of scope for GitOps (controller-created at runtime)

For each workspace the controller observes, it creates and reconciles:

- **`Deployment: agentfarm-daemon-<wsId>`** — 1 replica, image from
  `DAEMON_IMAGE`, mounts the per-workspace Secret + the shared ConfigMap +
  the per-workspace PVC at `/home/multica/multica_workspaces`.
  `MULTICA_DAEMON_ID=<wsId>`,
  `MULTICA_WORKSPACES_ROOT=/home/multica/multica_workspaces`. Resource
  requests sized for the v1 concurrency cap of 2 (see §6):
  `requests: {cpu: 500m, memory: 512Mi}`, `limits: {memory: 1Gi}`. Two
  concurrent CLI subprocesses fit inside the 1 GiB limit with margin; the
  shared ConfigMap's explicit `MAX_CONCURRENT_TASKS=2` provides backpressure
  before memory pressure becomes OOM. Resource numbers will be revisited
  after the smoke-test measurement in rollout step 1.

  Note on `MULTICA_DAEMON_ID=<wsId>`: workspace IDs are UUIDs (per the
  `workspaces` schema), and the server's `mergeLegacyRuntimes` path in
  `server/internal/handler/daemon.go` does case-insensitive UUID matching,
  so passing the wsId directly is safe. We'll add an explicit unit test in
  the controller package that asserts the daemon registers with the
  expected ID format end-to-end.
- **`ServiceAccount: agentfarm-daemon-<wsId>`** — IRSA-annotated to the
  per-workspace IAM role.
- **`PVC: agentfarm-daemon-<wsId>-workdir`** — `agentfarm-daemon-workdir`
  StorageClass, ~10 Gi default with room to grow via `volumeExpansion`. The
  daemon's GC already prunes old task dirs and regenerable artifacts, so
  10 Gi should be enough for typical workloads; we'll measure.
- **`ExternalSecret + Secret: agentfarm-daemon-<wsId>-secrets`** — daemon
  API token, server URL, and the LiteLLM-issued provider credentials (see
  §9). The daemon API token is scoped to the workspace by the server
  (single-workspace token, not a personal token); see §8 for how it's
  minted and refreshed.
- **`WorkspaceIAM` Composition CR** — the per-workspace Crossplane CR
  described in §4. Crossplane's provider pod reconciles this into the
  actual AWS role and policy.
- **LiteLLM team + virtual keys** for the workspace (see §9).

The controller never writes any of these manifests back to Git.

## 8. Daemon bootstrap (workspace ↔ daemon binding)

The most important question this proposal needs to answer: **how does a
fresh daemon pod end up connected to the right workspace, without a human
running `multica setup` / `multica login` / `multica daemon start`?** The
README's setup flow assumes an interactive operator on a laptop; for a
managed pod, the controller has to perform every step the operator
normally would, but server-side and idempotently.

A subtlety the original draft glossed over and code-checking caught:
**the daemon binary does not read `MULTICA_TOKEN` from the environment
today.** Only the auth subcommand does (`server/cmd/multica/cmd_auth.go`
`resolveToken`). The daemon's `resolveAuth`
(`server/internal/daemon/daemon.go:250`) only loads
`cli.LoadCLIConfigForProfile`, which reads `~/.multica/config.json`. So
"the daemon picks up an env var" is what we want, but not what we have.

Two ways to bridge this. Both are cheap; we recommend (a):

- **(a) Small daemon patch — preferred.** Add an env-var fallback in
  `resolveAuth`: if `cfg.Token` is empty, read `MULTICA_TOKEN` from the
  environment and use that. Mirrors what the auth subcommand already
  does, ~5 lines plus a test. Pod-friendly: env-only, no filesystem
  prep. This lands as a code prerequisite (§12.4).
- **(b) Entrypoint script writes `~/.multica/config.json`.** No daemon
  changes needed. The container's entrypoint reads `$MULTICA_TOKEN` and
  `$MULTICA_SERVER_URL` from env, writes them as JSON, then `exec`s
  `multica daemon start --foreground`. Slightly more moving parts and a
  bigger image surface (writable home directory).

With either path the controller's job is the same: mint the token, plant
it in the per-workspace Secret, let the daemon read it on start.

The bootstrap sequence the controller runs on workspace create:

1. **Mint a workspace-scoped daemon API token** by calling the agentfarm
   server's PAT-creation endpoint with the controller's own service-level
   credentials, scoped to the target workspace. This becomes the v1
   replacement for the `multica login` browser flow. (Today the server
   creates 90-day PATs in the OAuth flow; we'll add a server-side endpoint
   that mints a workspace-scoped token from a service-level credential —
   listed as a server-side prerequisite in §12.2.)
2. **Write the per-workspace Secret** with at minimum:
   - `MULTICA_TOKEN` — the workspace-scoped daemon token from step 1.
   - `MULTICA_SERVER_URL` — `https://agentfarm.g2.com`.
   - `ANTHROPIC_API_KEY` / `ANTHROPIC_BASE_URL` — from LiteLLM (§9).
   - `OPENAI_API_KEY` / `OPENAI_BASE_URL` — from LiteLLM (§9).
3. **Stand up the Deployment** with `envFrom: secretRef` pointing at that
   Secret plus `envFrom: configMapRef` pointing at the shared defaults
   ConfigMap (§6). Container command is `/app/multica daemon start
   --foreground`.
4. **Daemon registers itself.** On startup the daemon hits
   `GET /api/workspaces` (`server/internal/daemon/client.go:267`); the
   workspace-scoped token returns exactly one workspace (the one this
   daemon is bound to), and the daemon registers a runtime per detected
   CLI against that workspace. No watch list to configure — the token
   scope IS the watch list.

This means the only thing the bake/CI pipeline needs to know about
workspace identity is "build a generic image" — the workspace binding is
entirely runtime, materialized by the controller. There's no per-workspace
image, no baked-in workspace ID, and no setup script in the entrypoint.

**Token rotation.** PATs created via the server expire (today: 90 days).
The controller treats token refresh as part of its reconciliation loop:
when a token is within 7 days of expiry it mints a new one, replaces the
Secret entry, and Reloader rolls the pod onto the new credential. Old
tokens get explicitly revoked through the same endpoint, so a leaked token
has a strict upper bound on lifetime independent of expiry.

## 9. LiteLLM-issued provider credentials per runtime

Each per-workspace daemon pod gets a unique `ANTHROPIC_API_KEY` and
`OPENAI_API_KEY` minted by the controller against the LiteLLM proxy, plus
the matching `ANTHROPIC_BASE_URL` / `OPENAI_BASE_URL` pointing at the
proxy's provider-compatible endpoints. Every Claude or Codex call from a
workspace's daemon is tagged to that workspace in the LiteLLM dashboard at
`adoptai.g2.com`. No shared keys, no manual provisioning.

This is required for v1 because the daemon image only contains
`claude-code` and `codex`; both need their own provider credential, and
both will be routed through LiteLLM for billing and quota.

A new sub-reconciler in the controller, bound to the same `WorkspaceRuntime`
lifecycle as the rest of the per-workspace resources:

- **Create.**
  1. `POST /team/new` — create a LiteLLM team
     `agentfarm-workspace-<wsId>` (default: one team per workspace, see
     §12). The team carries an optional `max_budget` so spend caps are
     enforceable at the team level rather than the key level.
  2. For each provider in the v1 set (Anthropic, OpenAI), `POST /key/generate`
     with:
     - `key_alias: agentfarm-daemon-<wsId>-<provider>`
     - `team_id: <team-from-step-1>`
     - `models: ["claude-*"]` for Anthropic, `["gpt-*", "o*"]` for OpenAI —
       narrows blast radius if a CLI's model env var is misconfigured.
     - `metadata: {workspace_id: <wsId>, source: "agentfarm-runtime-controller"}`
       — same audit trail every other automation uses.
  3. Persist the returned `sk-...` values into the per-workspace Secret as
     `ANTHROPIC_API_KEY` / `OPENAI_API_KEY`, alongside the
     LiteLLM-resolved `ANTHROPIC_BASE_URL` / `OPENAI_BASE_URL`.
- **Pause** (workspace pause). `POST /key/update` with `blocked: true` and
  `max_budget: 0` for each key. Audit trail intact, no new spend possible.
- **Purge** (workspace purge). `POST /key/delete` for each key after the
  daemon Deployment is gone, then `POST /team/delete`. Order matters:
  revoke keys only after the pod is terminated, otherwise an in-flight tool
  call would 401 mid-task.
- **Rotate on demand.** Controller endpoint
  `POST /runtime/<wsId>/rotate-keys[?provider=anthropic|openai]` —
  generates a new key, updates the Secret, deletes the old key after
  Reloader rolls the pod.

Image impact: none. The CLI auth stays generic — `claude` reads
`ANTHROPIC_API_KEY` / `ANTHROPIC_BASE_URL`, `codex` reads `OPENAI_API_KEY` /
`OPENAI_BASE_URL`, and the per-workspace Secret materializes all four.
Adding a new CLI to the image (e.g. `cursor-agent` later) is a Dockerfile
bump plus one more provider in the controller's reconciler.

## 10. Workspace lifecycle

- **Create** — when a workspace is created in the server, an autopilot (or
  the server itself) calls the controller's API: "stand up runtime for
  workspace `<wsId>`". The controller creates, in order:
  1. Mint workspace-scoped daemon `MULTICA_TOKEN` via the server API (§8).
  2. LiteLLM team (`POST /team/new`).
  3. LiteLLM virtual keys for each provider (`POST /key/generate`).
  4. `WorkspaceIAM` Composition CR (Crossplane reconciles into AWS role).
  5. ServiceAccount → ExternalSecret → PVC → Deployment.
  Daemon comes up, registers `claude` and `codex` runtimes with the server,
  picks up tasks.
- **Update** (e.g. daemon image bump) — handled by the controller
  reconciliation loop. Bumping `DAEMON_IMAGE_TAG` in GitOps rolls all
  per-workspace Deployments to the new image. Per-workspace daemon
  overrides are not in scope for v1.
- **Delete** — explicit, two-phase:
  - **Pause** (reversible): scale Deployment to 0, daemon stops claiming
    tasks, LiteLLM keys blocked (`/key/update` with `blocked: true`),
    PVC + Secret retained.
  - **Purge** (irreversible). Order matters: tear down the consumer first,
    then identity, then storage, then external resources.
    1. Delete `Deployment` (pod terminates, no more in-flight tool calls).
    2. Delete `ExternalSecret` and `Secret`.
    3. Delete `ServiceAccount`.
    4. Delete `WorkspaceIAM` Composition CR. Crossplane reconciles the
       deletion into actual AWS `Role` + `Policy` removal — the controller
       does not call `iam:DeleteRole` itself.
    5. Delete `PVC`. Because the StorageClass uses `reclaimPolicy: Retain`,
       this leaves the underlying EBS volume **orphaned**. The next step
       handles it.
    6. Delete the orphaned EBS volume. The controller looks up the volume
       by tag (`agentfarm.workspace=<wsId>`, set on PVC creation via
       `volumeClaimTemplate` annotations that the AWS EBS CSI driver
       propagates to the volume) and calls `ec2:DeleteVolume`. The
       controller's IRSA grants exactly that one verb on tag-matched
       volumes — see §2. This is the only AWS API the controller calls
       directly; everything else goes through Crossplane or LiteLLM.
    7. Revoke LiteLLM credentials: `key/delete` for each provider key,
       then `team/delete`. Done last so a 401 mid-task is impossible.

  Two phases because deleting a workspace by accident shouldn't wipe
  in-progress work — pause is reversible, purge is not. We considered
  deferring EBS deletion to a manual runbook (cheaper to write) but
  rejected it: orphaned volumes accumulate quietly and bills grow without
  any signal until someone goes looking. Tag-scoped automation in the
  controller is the right home for this.

## 11. Daemon CLI selection

The daemon's existing CLI auto-detection in
`server/internal/daemon/identity.go` registers a runtime per detected CLI.
Once `claude` and `codex` are both on `PATH` inside the pod, the workspace
gets two runtimes — `claude` and `codex` — without further wiring.

If a workspace doesn't want one of them, that's controlled at the
runtime/agent layer (an agent in the workspace selects which runtime to
use), not at the image layer. The image stays uniform across workspaces.

## 12. Server-side prerequisites

These are not GitOps changes but they block implementation; calling them
out so they don't get missed in sprint planning. Each lands as its own PR
in `agentfarm/server/`:

1. **Workspace discovery endpoint.** The controller's "find workspaces
   that should have a runtime" call (`GET /v1/workspaces?has_runtime=true`,
   per §13.1) does not exist yet. Needs a server-side handler that filters
   workspaces by a new `runtime_enabled` flag (workspace settings JSONB)
   and returns the same shape `ListWorkspaces` already produces.
2. **Workspace-scoped daemon token issuance.** Today PATs are minted
   through the OAuth callback flow (`server/cmd/multica/cmd_auth.go:312`).
   We need a service-level endpoint the controller can hit with a
   trusted credential to mint a token bound to a single workspace, with a
   configurable TTL, and a corresponding revoke endpoint. This is the
   missing piece between §8 ("controller mints a `MULTICA_TOKEN`") and
   the existing daemon auth path.
3. **`runtime_enabled` workspace setting.** The flag the discovery
   endpoint filters on. Reuses the existing `workspaces.settings` JSONB
   column, exposed in workspace settings UI as a separate PR.
4. **Daemon `MULTICA_TOKEN` env-var fallback.** Tiny patch to
   `server/internal/daemon/daemon.go` `resolveAuth`: if the loaded CLI
   config has no token, fall back to `os.Getenv("MULTICA_TOKEN")` (the
   auth subcommand already does this). Unblocks pod-friendly headless
   auth — see §8.

Items 1, 2, and 4 are the real blockers. Item 3 can ship alongside the
controller without holding it back; the controller can default to
"runtime enabled for everyone" until the UI lands.

## 13. Open questions

1. **Workspace discovery surface.** Two options:
   - **a.** Controller calls the agentfarm server
     (`GET /v1/workspaces?has_runtime=true`) on a poll. Simple, no new
     types. Requires the server-side endpoint in §12.1.
   - **b.** Server reconciles a `WorkspaceRuntime` CR per workspace;
     controller watches the CR. Cleaner separation but adds a CRD and a
     server-side controller.

   Leaning (a) for v1: the server is already the source of truth and the
   controller doesn't need to handle CR validation/admission. Migrating to
   (b) later is straightforward.
2. **Concurrency / GC cap as a per-workspace knob.** v1 sets
   `MULTICA_DAEMON_MAX_CONCURRENT_TASKS=2` and `MULTICA_GC_TTL=6h`
   cluster-wide via the shared ConfigMap (§6). This is the safe default
   given the resource budget; future versions may surface both as
   per-workspace settings the controller injects into the per-workspace
   Secret. Not blocking v1.
3. **One controller pod or controller-per-cluster?** Given the tools
   cluster is single-region, a single leader-elected pod is fine.
   Multi-cluster expansion is post-v1.
4. **Repo cache placement.** The daemon currently caches repo bare clones
   at `<workspaces_root>/.repos`. PVC-backed is fine; we just want to
   confirm the 10 GiB workdir size budget accounts for it. The smoke test
   in rollout step 1 will measure.
5. **LiteLLM team granularity.** Defaulting to **one team per workspace**:
   the existing dashboard already aggregates by team, so this gives us a
   per-workspace leaderboard for free, and team-level `max_budget` makes
   spend caps simpler than per-key caps. Cost: one extra `team/new` on
   create, one extra `team/delete` on purge.
6. **Drift detection / repair (v2).** v1 reconciles existence only — see
   "v1 reconciliation behavior" in High-level shape. We accept that
   out-of-band edits to per-workspace resources persist until the
   resource is deleted. Open for v2:
   - Should the controller GET-then-PATCH on every reconcile to revert
     drift? Yes for security-relevant fields (IAM trust policy, key
     model scope), no for operator-tunable fields (resource limits)?
   - How do we surface drift events back to operators? Events on a
     status CR? A drift report endpoint?
   - On conflict, who wins — Git template or live spec?

   These are deferrable because the worst v1 failure modes (pod gone,
   PVC gone, Secret gone) are handled by the recreate-if-missing loop.
   Drift in the long tail is annoying, not load-bearing.

## 14. Rollout

0. Land the server-side prerequisites (§12) — workspace-scoped token
   issuance + revoke endpoints first (blocks the controller's bootstrap
   sequence), then the discovery endpoint, then the `runtime_enabled`
   flag wiring.
1. Land `Dockerfile.daemon` + `docker-bake.daemon.hcl` + publish workflow
   + a manual smoke pod (no controller yet) — proves the image runs the
   daemon end-to-end against the existing server, with `claude` and
   `codex` both registering. Use this run to measure actual peak memory
   per CLI subprocess and confirm the v1 resource budget in §1 + §6.
2. Land the controller Deployment + RBAC + Crossplane `Composition` for
   `WorkspaceIAM` + ExternalSecret/StorageClass templates + LiteLLM
   management-key ExternalSecret in `gitops/base/runtime-controller/`.
   Controller starts in **observe-only mode** — concretely:
   - The `--dry-run` flag (default `true` in step 2) gates every
     mutating call. The controller still runs the full reconcile loop
     against real workspace data from the server; for each resource it
     would create, update, or delete it logs a structured event
     (`action=create resource=Deployment workspace=<wsId>`,
     `action=delete resource=PVC workspace=<wsId>`, etc.) and emits the
     would-be manifest YAML at debug level.
   - Outbound calls that have side effects (Crossplane CR writes, k8s
     resource writes, LiteLLM `/team/new`, `/key/generate`,
     `ec2:DeleteVolume`) are stubbed: the call is logged, no API
     request is sent.
   - Read-only operations (server discovery, k8s GETs, LiteLLM list
     endpoints) run normally so the controller's view of "what would
     change" is grounded in real state.

   We can then review controller logs and confirm the planned actions
   look correct before flipping `--dry-run=false` for the pilot
   workspace in step 3.
3. Flip controller to write mode for one pilot workspace, exercise
   pause/purge end-to-end (including EBS volume reclamation, §10),
   validate IAM scoping with `aws iam simulate-principal-policy` against
   the Crossplane-created role, and confirm the LiteLLM team and keys
   appear in `adoptai.g2.com` tagged to the right workspace.
4. Open it up to all workspaces in the tools cluster.

Step 0 is server PRs; steps 1–2 are pure GitOps changes; steps 3–4 are
runbooks, no GitOps churn.
