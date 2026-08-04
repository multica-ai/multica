# Runtime And Agent Pause And Resume

This document is the contract for local/cloud runtime pause handling, agent-
scoped pause on multi-provider runtimes (Hermes), queued task expiry, and
automatic resume. Read it before changing pause/unpause, task queue sweepers,
claim routing, retry, or resume behavior.

## Two pause layers (FIR-4508)

| Layer | When | Effect |
|---|---|---|
| **Agent** (`agent.paused_at`) | Multi-provider runtime (Hermes) hits per-backend auth/quota/429 | Only that agent stops; siblings on the same runtime stay online |
| **Runtime** (`agent_runtime.paused_at`) | Single-provider runtime auth/quota, **or** true runtime death (`runtime_recovery`: gateway/provider unreachable, process down) | Whole runtime stops; no agent on it claims work |

Out of scope for v1: backend/credential-level pause shared across agents.

## Runtime Pause Contract

A paused runtime is a deliberate hold state, not an offline failure.

- The daemon claim path must not deliver work for agents on a paused runtime
  once claim guards are evaluated (queued tasks remain `queued`).
- Queued tasks on the paused runtime remain `queued` and wait for the runtime to
  unpause.
- Dispatched/running tasks are suspended as `failed` with
  `failure_reason='runtime_paused'`.
- Paused queued tasks may carry `wait_reason='runtime_paused|...'` so the UI can
  explain why work is waiting.
- Unpause clears the runtime pause fields and clears runtime-pause wait reasons
  on still-queued tasks.

## Agent Pause Contract (multi-provider)

A paused agent is a deliberate hold on **one** agent row.

- `ClaimTask` returns no task while `agent.paused_at` is set.
- Sibling agents on the same Hermes runtime keep claiming normally.
- Dispatched/running tasks for that agent are suspended as `failed` with
  `failure_reason='agent_paused'`.
- Queued tasks may carry `wait_reason='agent_paused|...'`.
- Auto-pause chooses agent scope when `agent_runtime.provider = hermes` and the
  failure is auth/quota/rate_limit — **not** when `failure_reason` would be
  `runtime_recovery` (that still pauses the runtime).
- Unpause clears agent pause fields, clears agent-pause wait reasons, and
  resumes leaf `agent_paused` / recent auth-quota failures for that agent only.

## Queued TTL Contract

The queued TTL sweeper exists to drain doomed backlog rows, especially old work
queued against offline runtimes. It must not drain paused runtime **or** paused
agent queues.

`ExpireStaleQueuedTasks` must keep both sides true:

- Stale queued tasks on non-paused runtimes/agents may expire as
  `failed/queued_expired`.
- Stale queued tasks on paused runtimes or paused agents must remain `queued`
  until unpause or cancel.

Do not key paused-queue protection only on `wait_reason`. Authoritative pause
state is `agent_runtime.paused_at` and `agent.paused_at`.

## Resume Contract

There are two resume paths per layer:

- Still-queued tasks are not recreated. They remain in the queue and are claimed
  normally after unpause (claim gate clears).
- Tasks that were already in flight when pause happened are recreated by the
  unpause path from failed parent rows (`runtime_paused` or `agent_paused`, plus
  transient failures in the pause-start window).

The unpause path intentionally skips autopilot run rows; autopilot cadence owns
its own rerun timing and must not double-fire.

## Invariants

- Runtime pause is scoped to one runtime. It must not pause sibling runtimes.
- Agent pause is scoped to one agent. It must not pause sibling agents or the
  shared multi-provider runtime on auth/quota.
- A queued task on a paused runtime or paused agent must survive longer than the
  generic queued TTL.
- Unpause must make preserved queued tasks claimable again without creating a
  duplicate child task.
- The queued TTL sweeper must keep its concurrency guards:
  `FOR UPDATE SKIP LOCKED`, status recheck, TTL recheck, and batch limit.
- Any change to this behavior needs regression coverage for both the paused
  queue and ordinary stale queue cleanup.

## Relevant Files

- `server/internal/handler/daemon.go` — claim endpoint.
- `server/internal/service/task.go` — claim gate, retry pause guards, auto-pause seam.
- `server/internal/cerebro/runtime/pause.go` — runtime pause/unpause + sweeper.
- `server/internal/cerebro/runtime/agent_pause.go` — agent pause/unpause + agent sweeper.
- `server/internal/cerebro/runtime/auto_pause.go` — auto-pause scope decision (agent vs runtime).
- `server/internal/cerebro/queries/runtime_pause.sql` / `agent_pause.sql` — SQL.
- `server/pkg/db/queries/agent.sql` — queued TTL, claim, retry lifecycle.
- `server/cmd/server/runtime_sweeper_test.go` — queued TTL regression tests.
- `server/internal/handler/runtime_pause_cerebro.go` / `agent_pause_cerebro.go` — HTTP.
