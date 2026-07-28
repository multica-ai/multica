# Provider Failover (Bidirectional, td-836aa9) — Implementation Spec

Task: `td-836aa9`. An explicit, auditable, fail-closed policy that hands a task
off between AI provider runtimes when — and only when — the source run
terminates on a **usage/rate-limit** condition. A runtime that stops proving
liveness is also classified for **shadow-only** analysis; it retains the
existing `runtime_offline` recovery behavior and can never create a
cross-provider continuation without terminal side-effect evidence. Ships
**shadow-first**: the default posture observes and records what it *would* do
without changing any task outcome or agent binding.

## td-836aa9 Cross-Arrangement Hardening (Four Invariants)

The initial implementation (GPT→Claude, actor-tier only, rate-limit triggers
only) has been hardened with four cross-arrangement invariants:

### 1. Authoritative liveness / heartbeat signal

A runtime that goes dark without returning a terminal provider error is
observed by the existing stale-runtime path:

- `pkg/taskfailure`: `ReasonProviderLivenessTimeout =
  "provider_liveness_timeout"` is an internal failover-policy signal, distinct
  from ordinary execution timeout.
- `cmd/server/runtime_sweeper.go`: `sweepStaleRuntimes` first checks the hot
  `LivenessStore`, marks only confirmed-dead runtimes offline, and then uses
  `FailTasksForOfflineRuntimes`.
- The task's persisted failure reason remains `runtime_offline`, preserving its
  existing bounded same-provider retry. Before that retry is created,
  `EvaluateLivenessFailover` evaluates started tasks with the refined
  `provider_liveness_timeout` signal.
- Liveness evaluations are always **shadow-only**, even when ordinary
  evidence-bearing callbacks are active. A dead runtime cannot produce a
  complete terminal side-effect observation, so an automatic continuation
  would risk duplicating unobserved effects.

There is deliberately no provider-specific task wall clock in this path.
Provider runtimes have different legitimate run lengths, and the runtime-wide
heartbeat cannot prove whether one task was silent. The LivenessStore-gated
runtime transition is the authoritative signal and avoids both killing a
healthy Claude run after 180 seconds and bypassing a fresh Redis heartbeat
because the database heartbeat lags.

### 2. Orchestrator-tier coverage

Failover was previously skipped for autopilot and leader tasks. Shadow coverage
is now extended to them, with an active-mode safety hold:

- `service/provider_failover.go`: `isOrchestratorTier(task)` returns true when
  `AutopilotRunID.Valid || IsLeaderTask`. `EvaluateFailover` no longer skips
  these tasks.
- `policy.go`: `Input.OrchestratorTier` and `Input.ControlPlaneIdempotent`
  fields. Active mode declines with `orchestrator_idempotency_unproven` unless
  `ControlPlaneIdempotent` is true. Shadow still records coverage.
- `controlPlaneIdempotentForChain` deliberately returns false. Child creation,
  stage promotion, mentions, assignee-triggered runs, squads, and autopilots
  form a larger control-plane effect surface than this change can prove
  replay-safe. Orchestrator-tier active handoff therefore remains closed;
  shadow data is used to design a complete idempotency contract later.

### 3. Control-plane fail-closed boundary

No partial control-plane ledger is installed and no ordinary issue write path
changes behavior when failover is off. A ledger covering only task spawn and
stage promotion would create a false proof while omitting other replayable
effects. Active orchestrator handoff remains unavailable until all
control-plane boundaries share a complete, transactional idempotency contract.

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
  `agent_error.provider_quota_limit` (402/usage-limit/quota/credits) — plus the
  server-owned `provider_liveness_timeout` shadow signal are the only failover
  triggers. Auth (`provider_auth_or_access`), ordinary timeouts, networking,
  server 5xx, context overflow, etc. are structurally excluded.
- **Provider is a property of the runtime** (`agent_runtime.provider`, the
  protocol/CLI family), not the task; a task binds a `runtime_id`. The
  OpenAI/GPT runtime provider is exactly **`codex`** (the Codex CLI) — there is
  no `openai`/`gpt` runtime provider in this repo (those are LLM *billing*
  strings). The failover source is a **closed whitelist `{codex, claude}`**
  (`providerfailover.IsFailoverSource`), not "anything that isn't Claude": the
  repo has ~17 runtime providers (grok, kimi, cursor, gemini, …) and only the
  two coding-plan runtimes are in scope. Failover re-targets a different,
  explicitly opted-in agent on the paired provider; it never mutates the
  original agent's binding.
- **The failure path** (`TaskService.FailTask`, `server/internal/service/task.go`)
  already computes a refined `failure_reason` and, for a small allow-list of
  reasons, atomically creates a retry child inside the fail transaction. Failover
  is a sibling of that path, but cross-provider, gated, and audited.
- **Status-CAS idempotency** (`UPDATE … WHERE status = 'running'`) already
  discards a second terminal transition on a task. Failover ownership adds an
  explicit, chain-level supersede guard on top of that CAS so a late primary
  completion is discarded deliberately, not incidentally.

The handoff ledger supplies durable task-chain ownership for ordinary
evidence-bearing task continuations. It does not claim to make orchestrator
replay safe.

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
  2. source provider not in the `{codex, claude}` whitelist → decline
     (`source_provider_not_failover_eligible`)
  3. task is itself a prior fallback → decline (loop prevention)
  4. chain already has an owning handoff → decline (at-most-one per chain)
  5. agent is authority-sensitive (`agent.kind = 'system'`,
     `runtime_config.provider_failover_protected=true`, or the exact legacy
     identity `Protected Reviewer`) → decline
     (structural exclusion)
  6. run was cancelled → decline
  7. observable side effects present → decline
  8. **(active only)** side-effect completeness not proven → decline
     (`side_effect_completeness_unproven`) — current daemons can prove it only
     by sending a completeness-marked terminal observation
  9. **(active only)** paired provider target unavailable →
     `HANDOFF_FAILED` (explicit)
  10. otherwise → `HANDOFF_PENDING` (active). In **shadow** mode every path is
     recorded as `HANDOFF_SHADOW` with the full "would/would-not, and why"
     verdict (steps 1–7 only; the active-only safety/availability gates do not
     apply), and no task outcome changes.

### New: persistence — migrations + sqlc

- `server/migrations/232_provider_failover_handoff.{up,down}.sql` — the ledger &
  ownership record `provider_failover_handoff`. No FKs (application-resolved),
  CHECK-constrained `state` and `mode`.
- `233…236_provider_failover_*_index.{up,down}.sql` — one `CREATE INDEX
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
  (DISPATCHED → COMPLETED/FAILED), list-paired-provider targets,
  create-fallback-task.
- Regenerated `server/pkg/db/generated/provider_failover.sql.go` + `models.go`.

### New: service wiring — `server/internal/service/provider_failover.go`

- `EvaluateFailover(ctx, task, failureReason)` — invoked best-effort after
  `FailTask` commits but **before** platform-authored failure comments/chat
  messages/notifications, only when `IsFailoverTrigger`. This prevents the
  side-effect scan from mistaking its own failure notification for an effect of
  the failed run. It gathers `Input`
  (side-effect snapshot, side-effect completeness, authority flags, chain state,
  paired-provider availability), calls `providerfailover.Decide`, and persists the ledger
  row. The active proceed path is **atomic**: a single `runInTx` records the
  owning row (ON CONFLICT / chain-owner unique index), creates the Claude
  fallback task, and advances `PENDING → DISPATCHED` with the `fallback_task_id`
  linkage — all commit together or none do, so a crash can never leave a queued
  child without persisted linkage, nor an owning row without a child. When
  the paired provider is unavailable it records `HANDOFF_FAILED` + posts a user-visible ledger
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
- Target resolution: an eligible paired-provider target is a non-archived,
  `kind='user'` agent in the same workspace on an online runtime. The target
  must have the **same owner** as the source agent, explicitly opt in with
  `runtime_config.provider_failover_target=true`, be non-authority-sensitive,
  and pass the exact same `CanInvokeAgent` permission predicate as an ordinary
  agent-to-agent dispatch. Database/permission uncertainty fails closed.

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
3. **Runtime liveness loss** (internal
   `provider_liveness_timeout` evaluation) — after the hot LivenessStore confirms
   a stale runtime is dead, a started task is persisted as `runtime_offline`,
   retains the normal bounded same-provider retry, and records a
   **shadow-only** failover decision. It never creates an active
   cross-provider continuation because terminal side-effect evidence is
   unavailable. A fresh LivenessStore heartbeat prevents the runtime and task
   from being reaped regardless of task age or provider.
4. **Timeout** (`agent_error.agent_timeout` / platform `timeout`) → **no**
   failover (`HANDOFF_DECLINED`, reason `not_a_failover_trigger`). A task wall
   clock does not prove provider liveness or an untouched side-effect surface;
   runtime-liveness loss is handled separately and remains shadow-only.
5. **Auth** (`provider_auth_or_access`) → **no** failover (declined,
   `not_a_failover_trigger`).
6. **Partial side-effect run** (observed delivered comment / agent comment) →
   **no** failover (declined, `side_effects_present`).
7. **Non-participant source** — a source runtime provider outside `{codex, claude}`
   (grok, gemini, kimi, …) → **no** failover (declined,
   `source_provider_not_failover_eligible`).
8. **Side-effect completeness (active safety hold)** — active mode proceeds only
   when a current daemon supplies a completeness-marked terminal observation
   with zero tool calls and no partial output. Older daemons and paths without
   that proof decline with `side_effect_completeness_unproven`; shadow still
   records the observable verdict.
9. **Orchestrator-tier (active safety hold)** — an orchestrator run (autopilot or
   leader task) always declines in **active** mode with
   `orchestrator_idempotency_unproven`. Shadow still records orchestrator
   coverage; no partial ledger is treated as proof.
10. **Cancellation** → **no** failover (cancelled tasks never reach the trigger;
    policy also declines `cancelled`).
11. **Late primary completion** — once a chain is owned by a handoff, a late
    `CompleteTask` on the original task is discarded. Fail-closed: if ownership
    cannot be read, `CompleteTask` returns the error and is retried. No
    `SUPERSEDED` state is written.
12. **Fallback lifecycle** — when the dispatched fallback finishes, its handoff
    row moves `DISPATCHED → COMPLETED`; when it fails, `DISPATCHED → FAILED`.
13. **At most one handoff per chain** — a second trigger and a fallback that hits
    a limit both decline (`max_one_handoff…` / `loop_prevented…`). Ownership
    claim is atomic (unique partial index); record-create-dispatch is one
    transaction.
14. **Target unavailable** → active mode yields explicit `HANDOFF_FAILED` and a
    user-visible ledger reference; the original task stays failed.
15. **Shadow default** — with only `provider_failover` on, no task outcome, agent
    binding, or dispatch changes; every decision is recorded as `HANDOFF_SHADOW`.
16. **Structural exclusion** — `agent.kind = 'system'` agents and user agents
    with `runtime_config.provider_failover_protected=true` are never handed off.
    The exact legacy identity `Protected Reviewer` is also excluded until older
    workspaces are marked; other reviewer-like names and `system_key` substrings
    are not authority markers. Reviewer runs that produced an authoritative
    verdict are also excluded by the side-effect gate.
17. **Authorization boundary** — a target must share the source owner, carry
    `provider_failover_target=true`, and pass ordinary invocation permissions.
    A foreign/private agent, member-only target for a different originator, or
    failed membership lookup cannot receive a continuation.
18. **Exactly once from the real failure path** — a clean quota failure creates
    one owning ledger row and one continuation. Repeating the terminal callback
    creates neither a second row nor a second task.

## Migration numbering

Current upstream owns migrations through 231. This change therefore uses one
unambiguous sequence:

- 232: handoff table;
- 233–236: isolated concurrent handoff indexes.

Every production index is created concurrently in its own migration. Do not
reuse or collapse this sequence when merging a newer upstream; renumber it
above the then-current upstream maximum if that maximum has advanced.

## Shadow-first rollout and rollback

1. Deploy the migrations and server with `FF_PROVIDER_FAILOVER=true` and
   `FF_PROVIDER_FAILOVER_ACTIVE=false`. This is also the self-host compose
   default.
2. Opt in only paired-provider targets whose owner/credentials/tools are
   intended for automatic continuation by setting
   `runtime_config.provider_failover_target=true`. Keep protected reviewers and
   authority-bearing agents opted out/protected.
3. Observe `GET /api/issues/{id}/failover-handoffs` and aggregate ledger
   `reason`, source/target provider, and `would_fail_over`. Confirm there are no
   cross-owner candidates and no unexpected side-effect declines.
4. Enable `FF_PROVIDER_FAILOVER_ACTIVE=true` only after the shadow sample is
   reviewed and current daemons are confirmed to send complete side-effect
   evidence. Runtime-liveness and orchestrator-tier decisions remain
   shadow-only.
5. Roll back real handoffs immediately with
   `FF_PROVIDER_FAILOVER_ACTIVE=false`; the audit ledger continues in shadow.
   Set `FF_PROVIDER_FAILOVER=false` to disable evaluation entirely. Existing
   dispatched tasks and ledger rows are not rewritten by either switch.

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
