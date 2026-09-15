# Issue wakeups

An agent can save an event subscription or a timer on an issue, finish its run,
and receive another ordinary run when the input arrives. Business completion is
still decided by the agent after reading current state. There is no sleeping
process, business-condition evaluator, or second run lifecycle.

## Product contract

The shared web/Desktop issue sidebar lists event and time wakeups together,
including the target agent, instruction, source, next time and latest run.
The switch withdraws a configuration's unclaimed work. Running or already
claimed tasks use the normal Stop controls. Terminal lifecycle categories
(`done` and `closed`, including custom statuses) disable all configurations;
reopening does not reactivate them.

Agents manage configurations with `multica issue wakeup`:

```sh
multica issue wakeup events
multica issue wakeup create ISSUE --kind at --after 10m --instruction-file ./instruction.md
multica issue wakeup create ISSUE --kind every --every 1h --instruction-file ./instruction.md
multica issue wakeup create ISSUE --kind cron --cron '0 * * * *' --timezone Asia/Shanghai --instruction-file ./instruction.md
multica issue wakeup create ISSUE --kind event --event task.completed,task.failed,task.cancelled --task-id RUN --instruction-file ./instruction.md
multica issue wakeup list ISSUE
multica issue wakeup get ISSUE WAKEUP
multica issue wakeup disable ISSUE WAKEUP
```

Specify `--agent-id` for human callers; authenticated agents default to themselves.
Event subscriptions default to `once`; `--mode continuous` keeps listening.
A concrete source run must belong to this issue. Registration checks its current
terminal state under a lock, so a run that just finished is not missed. A busy
source run returns 409 and asks the caller to retry; registration never waits
on a source lock while holding the issue lock. Broad
agent filters observe future facts, without replaying history or following retry
chains. `--parent COMMENT` preserves the original result-delivery thread.

`update ISSUE WAKEUP` accepts the complete create configuration, replaces it,
increments its revision, withdraws old unclaimed inputs, and explicitly enables
it. The creator or a workspace owner/admin may update or disable; the replacing
caller must themselves be allowed to invoke the selected agent and becomes the
new configuration's recorded human principal.

The scheduler checks approximately every 30 seconds. One-shot timers remain
queued while their runtime is offline. Repeating timers coalesce missed periods
into one pending check and continue from the next future time; they do not replay
every historical tick. All runs retain normal comment delivery, including checks
that find no change. CI can be polled by the agent; CI push events are not claimed
as supported by this version.

## Implementation

- `pkg/eventcontract` owns stable business-event names independently of plugins.
  Plugin constants alias the existing names, preserving their payload contract.
- `issue_wakeup` stores configuration and `issue_wakeup_receipt` stores matching
  inputs. Task terminal transitions, comment creation and issue status changes
  capture matching receipts in the source transaction. SQL capture hooks cover
  service, scheduler and HTTP writers without a best-effort in-memory hop. They
  do not build a general event archive or evaluate business predicates.
- Registration is prospective once committed; agents should subscribe before
  querying current state. The explicit run filter also checks current state
  during registration. There is no global event order or historical replay API.
- The existing scheduler leases `issue_wakeup_dispatch`. The adapter locks one
  issue/configuration, rechecks scope and the creator's current invoke rights,
  and consumes receipts together with ordinary task enqueue. Failure leaves the
  receipt available for retry. Claim rechecks permissions after an offline wait.
- Event inputs merge into an unclaimed task without losing their references.
  Time inputs replace the pending time note with the newest signal. A claimed
  prompt is immutable; later input becomes at most one subsequent queued task.
- Direct comments and task/status events from the same wakeup's run are ignored.
  The trusted task source of HTTP issue updates is transaction-local; a client
  cannot choose it through the issue request body.
- `context.wakeup_id` and `context.wakeup_revision` identify the new trigger.
  Its instruction/facts travel in the ordinary per-turn handoff note. Daemon
  wakeup prompts preserve this instruction even when a delivery thread exists.
  Automatic retries inherit both context and note.
- The existing pending-task index is retained. For wakeup tasks only, its derived
  `comment_thread_id` scheduling scope is the configuration ID; the real delivery
  thread stays in `trigger_comment_id`. This preserves old retry SQL during a
  rolling server upgrade. Comment/assign coalescing excludes wakeup inputs, and
  the existing issue/agent execution fence still serializes actual runs.
- Issue/workspace deletion explicitly removes configurations and receipts in
  the application deletion graph. No foreign keys or cascading relationships
  are added.

## Deployment and verification

Apply additive migrations before starting the new server. Existing pending-task
indexes are not rebuilt or dropped, and historical queue rows are not rewritten.
Deploy the updated CLI and daemon with the server to recognize the wakeup command
and per-turn prompt. Before rollback, disable/drain wakeups; do not remove their
configuration tables while tasks still reference them.

Integration coverage includes transaction rollback, already-terminal registration,
source scope, one-shot deduplication, independent comment/assign input, merging,
self-loop suppression, custom terminal statuses, reopen behavior, offline timers,
configuration replacement, revoked permission at enqueue/claim, and retry prompt
inheritance. UI tests cover disabling and consumed one-shot state; API response
schemas reject malformed wakeup state rather than presenting an empty list.
