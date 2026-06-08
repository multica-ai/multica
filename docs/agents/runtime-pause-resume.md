# Runtime Pause And Resume

This document is the contract for local/cloud runtime pause handling, queued
task expiry, and automatic resume. Read it before changing runtime pause,
task queue sweepers, claim routing, retry, or resume behavior.

## Runtime Pause Contract

A paused runtime is a deliberate hold state, not an offline failure.

- The daemon claim endpoint returns no task while `agent_runtime.paused_at` is
  set.
- Queued tasks on the paused runtime remain `queued` and wait for the runtime to
  unpause.
- Dispatched/running tasks are suspended as `failed` with
  `failure_reason='runtime_paused'`.
- Paused queued tasks may carry `wait_reason='runtime_paused|...'` so the UI can
  explain why work is waiting.
- Unpause clears the runtime pause fields and clears runtime-pause wait reasons
  on still-queued tasks.

## Queued TTL Contract

The queued TTL sweeper exists to drain doomed backlog rows, especially old work
queued against offline runtimes. It must not drain paused runtime queues.

`ExpireStaleQueuedTasks` must keep both sides true:

- Stale queued tasks on non-paused runtimes may expire as
  `failed/queued_expired`.
- Stale queued tasks on paused runtimes must remain `queued` until the runtime
  is unpaused or the task is otherwise cancelled/superseded.

Do not key paused-queue protection only on `wait_reason`. The authoritative
pause state is `agent_runtime.paused_at`; `wait_reason` is display metadata.

## Resume Contract

There are two resume paths:

- Still-queued tasks are not recreated. They remain in the queue and are claimed
  normally after unpause.
- Tasks that were already in flight when pause happened are recreated by the
  unpause path from failed parent rows.

The unpause resume query includes:

- `runtime_paused` rows, because pause itself interrupted them.
- Transient failures in the pause-start window (`rate_limit`,
  `runtime_offline`, `runtime_recovery`, `timeout`), because those failures may
  have triggered the pause.

The unpause path intentionally skips autopilot run rows; autopilot cadence owns
its own rerun timing and must not double-fire.

## Invariants

- Pause is scoped to one runtime. It must not pause sibling runtimes in the same
  workspace.
- A queued task on a paused runtime must survive longer than the generic queued
  TTL.
- Unpause must make preserved queued tasks claimable again without creating a
  duplicate child task.
- The queued TTL sweeper must keep its concurrency guards:
  `FOR UPDATE SKIP LOCKED`, status recheck, TTL recheck, and batch limit.
- Any change to this behavior needs regression coverage for both the paused
  queue and ordinary stale queue cleanup.

## Relevant Files

- `server/internal/handler/daemon.go` — claim endpoint pause gate.
- `server/internal/cerebro/runtime/pause.go` — pause/unpause service behavior.
- `server/internal/cerebro/queries/runtime_pause.sql` — pause and resume
  queries.
- `server/pkg/db/queries/agent.sql` — queued TTL, claim, retry, and task
  lifecycle queries.
- `server/cmd/server/runtime_sweeper.go` — runtime/task sweeper loop.
- `server/cmd/server/runtime_sweeper_test.go` — queued TTL regression tests.
