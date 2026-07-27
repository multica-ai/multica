# Provider Failover (Bidirectional, td-836aa9) — Implementation Spec

Task: `td-836aa9`. An explicit, auditable, fail-closed policy that hands a task
off between AI provider runtimes when — and only when — the source run
terminates on a **usage/rate-limit** condition OR goes **silently unresponsive**
past a wall-clock liveness deadline. Ships **shadow-first**: the default posture
observes and records what it *would* do without changing any task outcome or
agent binding.

## td-836aa9 Cross-Arrangement Hardening (Four Invariants)

The initial implementation (GPT→Claude, actor-tier only, rate-limit triggers
only) has been hardened with four cross-arrangement invariants:

### 1. Liveness / heartbeat watchdog

A SILENT HANG — where the provider process wedges without returning any error
— now triggers handoff just as a rate-limit error does. New:

- `pkg/taskfailure`: `ReasonProviderLivenessTimeout = "provider_liveness_timeout"`,
  added to `IsFailoverTrigger`. Distinct from `ReasonAgentTimeout` (busy process
  that hit a hard cap) — here the process is **wedged**, not working.
- `pkg/providerfailover/liveness.go`: `LivenessDeadline(provider)` (60min for
  codex, 180s for claude), `IsSilentHang(provider, runningFor, heartbeatAlive)`.
- `pkg/db/queries/agent.sql` + generated Go: `FailSilentHangTasks` — UPDATEs
  running tasks whose runtime is stale AND which have exceeded the
  provider-specific deadline, sets `failure_reason = 'provider_liveness_timeout'`.
- `cmd/server/runtime_sweeper.go`: `sweepSilentHangTasks` (constants
  `codexLivenessSecs = 3600`, `claudeLivenessSecs = 180`) fires before
  `sweepStaleTasks` in each 30s tick; calls `EvaluateLivenessFailover` on
  reaped tasks.

### 2. Orchestrator-tier coverage

Failover was previously skipped for autopilot and leader tasks. Coverage is now
extended to them, gated by an active-mode safety hold:

- `service/provider_failover.go`: `isOrchestratorTier(task)` returns true when
  `AutopilotRunID.Valid || IsLeaderTask`. `EvaluateFailover` no longer skips
  these tasks.
- `policy.go`: `Input.OrchestratorTier` and `Input.ControlPlaneIdempotent`
  fields. Active mode declines with `orchestrator_idempotency_unproven` unless
  `ControlPlaneIdempotent` is true. Shadow still records coverage.
- `controlPlaneIdempotentForChain` returns `false` unconditionally (documented
  fail-closed placeholder) until task-spawn/stage-promotion call sites adopt
  `ClaimControlPlaneEffectOnce`.

### 3. Control-plane idempotency ledger

Prevents a handed-off fallback from double-spawning children or
double-promoting stages that the failed orchestrator already dispatched:

- `migrations/229_control_plane_effect_ledger.up.sql`: table
  `control_plane_effect_ledger` with `UNIQUE effect_key`.
- `pkg/providerfailover/controlplane.go`: `EffectKey(chainRoot, effect, target)`
  (SHA256 of NUL-separated components), `ControlPlaneEffect` enum
  (`task_spawn`, `stage_promotion`).
- `service/provider_failover.go`: `ClaimControlPlaneEffectOnce` — at-most-once
  INSERT ON CONFLICT DO NOTHING; callers that get `pgx.ErrNoRows` skip the
  already-dispatched effect.
- `pkg/db/queries/provider_failover.sql`: `ClaimControlPlaneEffect`,
  `GetControlPlaneEffect`.

### 4. Bidirectional policy

Failover is now role/capacity-based in both directions (codex→claude AND
claude→codex) rather than a unidirectional codex whitelist:

- `pkg/providerfailover/classify.go`: `failoverProviders = {codex, claude}`,
  `failoverTargets = {codex: [claude], claude: [codex]}`.
- `IsFailoverSource`, `FailoverTargets`, `PrimaryTargetFor` all work
  bidirectionally. `TargetProvider = "claude"` kept for backward compat.
- `pkg/db/queries/provider_failover.sql`: `ListFailoverTargets` takes
  `@target_provider` (was `ListClaudeFailoverTargets`, hardcoded).
- Loop/ping-pong prevention is structural (at-most-one-per-chain + already-a-
  fallback guard), not directional.

## Why these choices

The backend already has every primitive this needs; we compose them rather than
invent parallel machinery:

- **Failure classification** is centralized in `server/pkg/taskfailure`
  (`Classify(error) Reason`). The two provider-limit reasons —
  `agent_error.provider_capacity_or_rate_limit` (429/529/overloaded) and
  `agent_error.provider_quota_limit` (402/usage-limit/quota/credits) — are the
  *only* failover triggers. Auth (`provider_auth_or_access`), timeouts,
  networking, server 5xx, context overflow, etc. are structurally excluded.
- **Provider is a property of the runtime** (`agent_runtime.provider`, the
  protocol/CLI family), not the task; a task binds a `runtime_id`. The
  OpenAI/GPT runtime provider is exactly **`codex`** (the Codex CLI) — there is
  no `openai`/`gpt` runtime provider in this repo (those are LLM *billing*
  strings). The failover source is a **closed whitelist `{codex}`**
  (`providerfailover.IsFailoverSource`), not "anything that isn't Claude": the
  repo has ~17 runtime providers (grok, kimi, cursor, gemini, …) and only the
  GPT one is in scope. Failover re-targets a *different* agent whose runtime
  provider is `claude`; it never mutates the original agent's binding.
- **The failure path** (`TaskService.FailTask`, `server/internal/service/task.go`)
  already computes a refined `failure_reason` and, for a small allow-list of
  reasons, atomically creates a retry child inside the fail transaction. Failover
  is a sibling of that path, but cross-provider, gated, and audited.
- **Status-CAS idempotency** (`UPDATE … WHERE status = 'running'`) already
  discards a second terminal transition on a task. Failover ownership adds an
  explicit, chain-level supersede guard on top of that CAS so a late primary
  completion is discarded deliberately, not incidentally.

There is **no** existing side-effect ledger or provider-failover concept in the
repo; both are introduced here.

## Components / files

### New: policy engine — `server/pkg/providerfailover/` (pure, no DB)

The auditable core. Deterministic, table-tested, no I/O.

- `classify.go` — `IsFailoverTrigger(taskfailure.Reason) bool`. True only for the
  two provider-limit reasons above. Single source of truth for "is this a
  usage/rate-limit failure".
- `sideeffects.go` — `SideEffects` snapshot + `HasObservableSideEffects()`.
  Presence of *any* observed tool call, delivered comment, agent comment,
  advanced head SHA, or partial user-facing output blocks fallback. Presence is
  authoritative; **absence is not proof** — only delivered-comments and
  agent-commented are reconstructable server-side today. The other three are
  daemon-side and unplumbed, which is why active mode adds a completeness gate
  (below).
- `state.go` — `HandoffState` enum and the legal transition graph
  (`CanTransition`). States: `HANDOFF_PENDING`, `HANDOFF_DISPATCHED`,
  `HANDOFF_COMPLETED`, `HANDOFF_FAILED`, `HANDOFF_SHADOW`, `HANDOFF_DECLINED`.
  The only live transitions are `PENDING → DISPATCHED|FAILED` and
  `DISPATCHED → COMPLETED|FAILED`; `SHADOW`/`DECLINED` are recorded terminal.
  (There is deliberately no `SUPERSEDED` state: a late primary completion is
  discarded by the CompleteTask guard **without** releasing the fallback's chain
  ownership, so no ledger transition is warranted.)
- `policy.go` — `Decide(Input) Decision`. Pure decision, in fail-closed order:
  1. not a failover trigger → decline (`not_a_failover_trigger`)
  2. source provider not in the `{codex}` whitelist → decline
     (`source_provider_not_failover_eligible`) — covers Claude itself and every
     non-GPT CLI
  3. task is itself a prior fallback → decline (loop prevention)
  4. chain already has an owning handoff → decline (at-most-one per chain)
  5. agent is authority-sensitive (`agent.kind = 'system'`,
     `runtime_config.provider_failover_protected=true`, or the exact legacy
     identity `Protected Reviewer`) → decline
     (structural exclusion)
  6. run was cancelled → decline
  7. observable side effects present → decline
  8. **(active only)** side-effect completeness not proven → decline
     (`side_effect_completeness_unproven`) — the current fail-closed posture,
     since the server cannot prove absence of tool calls / head movement /
     partial output
  9. **(active only)** Claude unavailable → `HANDOFF_FAILED` (explicit)
  10. otherwise → `HANDOFF_PENDING` (active). In **shadow** mode every path is
     recorded as `HANDOFF_SHADOW` with the full "would/would-not, and why"
     verdict (steps 1–7 only; the active-only safety/availability gates do not
     apply), and no task outcome changes.

### New: persistence — migrations + sqlc

- `server/migrations/224_provider_failover_handoff.{up,down}.sql` — the ledger &
  ownership record `provider_failover_handoff`. No FKs (application-resolved),
  CHECK-constrained `state` and `mode`.
- `225…228_provider_failover_*_index.{up,down}.sql` — one `CREATE INDEX
  CONCURRENTLY` per file:
  - `…_original_task_uidx` — unique on `original_task_id`: one ledger row per
    failed task (idempotent record).
  - `…_chain_owner_uidx` — unique on `chain_root_task_id` **partial** to the
    owning states (`PENDING`/`DISPATCHED`/`COMPLETED`): enforces at-most-one
    active fallback per task chain.
  - `…_issue_idx` — issue lookup for the read API.
  - `…_fallback_task_idx` — reverse lookup (is-this-task-a-fallback / finalize
    the handoff when its fallback task completes or fails).
- `server/pkg/db/queries/provider_failover.sql` — sqlc queries: record (ON
  CONFLICT DO NOTHING), chain-has-owning-handoff, get-by-original,
  get-by-fallback, list-for-issue, CAS state transition, finalize-by-fallback
  (DISPATCHED → COMPLETED/FAILED), list-Claude-targets, create-fallback-task.
- Regenerated `server/pkg/db/generated/provider_failover.sql.go` + `models.go`.

### New: service wiring — `server/internal/service/provider_failover.go`

- `EvaluateFailover(ctx, task, failureReason)` — invoked best-effort *after*
  `FailTask` commits, only when `IsFailoverTrigger`. Gathers `Input`
  (side-effect snapshot, side-effect completeness, authority flags, chain state,
  Claude availability), calls `providerfailover.Decide`, and persists the ledger
  row. The active proceed path is **atomic**: a single `runInTx` records the
  owning row (ON CONFLICT / chain-owner unique index), creates the Claude
  fallback task, and advances `PENDING → DISPATCHED` with the `fallback_task_id`
  linkage — all commit together or none do, so a crash can never leave a queued
  child without persisted linkage, nor an owning row without a child. When
  Claude is unavailable it records `HANDOFF_FAILED` + posts a user-visible ledger
  reference.
- `FinalizeFailoverForFallbackOutcome(ctx, taskID, terminal)` — called
  post-commit from `CompleteTask`/`FailTask` for the **fallback** task (keyed by
  `fallback_task_id`); advances `DISPATCHED → COMPLETED` (complete) or
  `DISPATCHED → FAILED` (fail) so the ledger's lifecycle stays accurate. No-op
  for non-fallback tasks.
- `OriginalTaskSuperseded(ctx, taskID) (bool, error)` — consulted by
  `CompleteTask`; a late primary completion whose chain is owned by a handoff is
  discarded (no comment, no resurrection). **Fail-closed**: `pgx.ErrNoRows` →
  not superseded (safe to complete); any other lookup error → the error
  propagates and `CompleteTask` returns it, so the completion is retried rather
  than risking a duplicate outcome under ownership uncertainty.
- `failoverSideEffectsComplete(evidence)` — requires a completeness-marked
  daemon observation. Tool-call counts and partial streamed output are carried
  to the server fail path; any observed tool call or partial user output blocks
  an active handoff.
- Target resolution: an eligible Claude agent is a non-archived, `kind='user'`
  agent in the same workspace whose runtime is `online` with provider `claude`;
  the service additionally drops any `agent.kind='system'` candidate.

### Wiring edits (minimal, gated)

- `server/internal/featureflags/keys.go` — add `provider_failover` (default off →
  shadow when on) and `provider_failover_active` (rollout gate for real
  handoffs). Two-stage: shadow → active.
- `server/internal/service/task.go` — call `EvaluateFailover` +
  `FinalizeFailoverForFallbackOutcome(FAILED)` from `FailTask` (post-commit,
  non-fatal), and `OriginalTaskSuperseded` (fail-closed) +
  `FinalizeFailoverForFallbackOutcome(COMPLETED)` from `CompleteTask`.
- `server/internal/handler/provider_failover.go` — read-only JSON API
  (`GET /api/issues/{id}/failover-handoffs`) returning the handoff ledger for an
  issue. This is the observability surface; there is **no dedicated UI**. The
  active dispatch/unavailable paths additionally post a system comment on the
  issue, which is the user-facing signal.

## Acceptance criteria

1. **429 / rate-limit** (`provider_capacity_or_rate_limit`) on a codex or claude
   run with no side effects → policy elects failover (`HANDOFF_PENDING` active /
   recorded in shadow). Bidirectional: codex→claude AND claude→codex.
2. **Usage/quota limit** (`provider_quota_limit`) → same as (1).
3. **Silent hang / liveness timeout** (`provider_liveness_timeout`) — a running
   task whose runtime is stale AND which has exceeded the provider-specific
   wall-clock deadline (codex: 60min, claude: 180s) → policy elects failover.
   `sweepSilentHangTasks` fails the task first; `EvaluateLivenessFailover` then
   evaluates it as a handoff candidate.
4. **Timeout** (`agent_error.agent_timeout` / platform `timeout`) → **no**
   failover (`HANDOFF_DECLINED`, reason `not_a_failover_trigger`). Plain timeout
   means the process was actively working; silent hang means it was wedged.
5. **Auth** (`provider_auth_or_access`) → **no** failover (declined,
   `not_a_failover_trigger`).
6. **Partial side-effect run** (observed delivered comment / agent comment) →
   **no** failover (declined, `side_effects_present`).
7. **Non-participant source** — a source runtime provider outside `{codex, claude}`
   (grok, gemini, kimi, …) → **no** failover (declined,
   `source_provider_not_failover_eligible`).
8. **Side-effect completeness (active safety hold)** — even an otherwise-eligible
   run declines in **active** mode (`side_effect_completeness_unproven`) because
   the server cannot prove absence of in-run tool calls / head movement / partial
   output. Shadow still records the observable would-fail-over verdict.
9. **Orchestrator-tier (active safety hold)** — an orchestrator run (autopilot or
   leader task) declines in **active** mode with `orchestrator_idempotency_unproven`
   unless `ControlPlaneIdempotent` is proven (currently always false). Shadow
   still records orchestrator coverage so it is observable before enablement.
10. **Control-plane idempotency** — `ClaimControlPlaneEffectOnce` (INSERT ON
    CONFLICT DO NOTHING by `effect_key`) ensures a handed-off fallback that
    re-plans cannot double-spawn children or double-promote stages the failed
    orchestrator already dispatched.
11. **Cancellation** → **no** failover (cancelled tasks never reach the trigger;
    policy also declines `cancelled`).
12. **Late primary completion** — once a chain is owned by a handoff, a late
    `CompleteTask` on the original task is discarded. Fail-closed: if ownership
    cannot be read, `CompleteTask` returns the error and is retried. No
    `SUPERSEDED` state is written.
13. **Fallback lifecycle** — when the dispatched fallback finishes, its handoff
    row moves `DISPATCHED → COMPLETED`; when it fails, `DISPATCHED → FAILED`.
14. **At most one handoff per chain** — a second trigger and a fallback that hits
    a limit both decline (`max_one_handoff…` / `loop_prevented…`). Ownership
    claim is atomic (unique partial index); record-create-dispatch is one
    transaction.
15. **Target unavailable** → active mode yields explicit `HANDOFF_FAILED` and a
    user-visible ledger reference; the original task stays failed.
16. **Shadow default** — with only `provider_failover` on, no task outcome, agent
    binding, or dispatch changes; every decision is recorded as `HANDOFF_SHADOW`.
17. **Structural exclusion** — `agent.kind = 'system'` agents and user agents
    with `runtime_config.provider_failover_protected=true` are never handed off.
    The exact legacy identity `Protected Reviewer` is also excluded until older
    workspaces are marked; other reviewer-like names and `system_key` substrings
    are not authority markers. Reviewer runs that produced an authoritative
    verdict are also excluded by the side-effect gate.

## Legacy self-host migration collision

The first private self-host rollout shipped the failover schema under migration
stems `212_provider_failover_handoff` through
`217_control_plane_effect_ledger`. Upstream later assigned numeric prefixes
212–217 to unrelated agent usage, chat-project, and VCS migrations. Migration
identity is the full filename stem, so both sets can coexist in
`schema_migrations`, but the duplicate-prefix source tree violates migration
lint and can leave an affected self-host database missing the upstream schema.

The durable numbering is now:

- upstream migrations retain 212–223;
- provider failover uses 224–229;
- failover migrations 224–229 are idempotent so an affected database can record
  the corrected stems without recreating already-present failover tables and
  indexes.

Before upgrading an affected database, apply upstream migrations 212–217 and
record their full stems, then run the normal migrator. The normal run applies
upstream 218–223 followed by failover 224–229. Validate this sequence on a
database clone first; do not infer correctness from the highest numeric prefix,
because readiness checks full stems and out-of-order migrations are valid.

## Fail-closed notes / documented safety decisions

- Failover only ever *reads* the original run's outcome and *adds* a new,
  separately-owned paired-provider task; it never re-completes or resurrects
  the original.
- Any uncertainty resolves to "do not fail over" (active mode): incomplete
  daemon evidence, an unreadable chain ownership, a missing availability signal,
  or an unresolved target all decline or hold. Completeness-marked daemon
  evidence supplies the tool-call and partial-output proof; older-daemon and
  liveness-sweeper paths remain fail-closed.
- The failover source is a closed whitelist (`{codex, claude}`), so a widening
  of runtime providers never silently expands what can be handed off.
- Shadow is the default and is purely observational; active handoffs require a
  second, explicit rollout gate **and** proven side-effect completeness.
- The pure policy engine holds the authoritative, exhaustive tests; DB queries
  are verified by `sqlc generate` + compilation; the wiring is gated so the
  default build path is unchanged.
