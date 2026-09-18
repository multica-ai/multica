# Issue wakeups

An agent can save an event subscription or a timer on an issue, finish its run,
and receive another ordinary run when the input arrives. Business completion is
still decided by the agent after reading current state. There is no sleeping
process, business-condition evaluator, or second run lifecycle.

## Product contract

The shared web/Desktop issue sidebar lists event and time wakeups together,
as compact trigger/target summaries with a separate execution status when available.
Instructions, full filters, errors and
latest-run transcript are in the details popover. A directly visible toggle
controls enabled configurations and restores manually disabled subscriptions or
recurring schedules. Consumed one-shots offer Cancel this execution while unclaimed,
then Enable again (events) or Set a new time (time).
Wakeup history is collapsed by default. A consumed one-shot remains
in the current group while its run is queued, deferred, dispatched or running.
Running or already
claimed tasks use the normal Stop controls. Terminal lifecycle categories
(`done` and `closed`, including custom statuses) disable all configurations;
reopening does not reactivate them.

Board and list activity cues prioritize current runs, mark wakeup-origin runs,
and show enabled wakeup rules as a separate count, not a forecast of executions.
Without an active run they show the
next scheduled time (including a date when needed) or waiting for an event.
A shared workspace summary request contains exact enabled counts and at most
three previews per issue, never prompts or history. Access follows agent
visibility and workspace membership. The issue surface polls once every ten
seconds; cards select their own rows from that shared cache.

Restoring an interval schedules from now; cron uses its next future occurrence,
without replaying missed times. An unconsumed future one-shot can be toggled back
on; expired or consumed time wakeups require choosing a new future time. One-shot
rearming is refused while a previous run is active. Closed issues cannot enable
wakeups; reopening an issue still requires manual enable.

The additive `POST /api/issues/{id}/wakeups/{wakeupID}/enable` accepts the observed
`revision`, plus optional `rearm` and future `at`. It loads stored configuration
under the existing issue/configuration locks and reuses Save's validation,
authorization, receipt cleanup and revision fencing. A stale revision returns
409, and a repeated already-enabled request with the current revision is a no-op.
Clients do not resend instructions, filters or thread references. The new UI
requires this endpoint for restore; deploy the server first. Old clients and the
existing full-config CLI update continue to work without a migration.

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
on a source lock while holding the issue lock. The server returns the stable
`wakeup_source_busy` code for this rolled-back conflict; CLI create retries it
once after 250ms. Other conflicts and ambiguous network failures are not retried.
Broad
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

## Event catalog

`multica issue wakeup events` lists the 25 supported issue-scoped subscriptions:

| Area | Events |
| --- | --- |
| Run | `task.queued`, `task.dispatched`, `task.started`, `task.deferred`, `task.waiting_local_directory`, `task.completed`, `task.failed`, `task.cancelled` |
| Issue | `issue.updated`, `issue.status_changed`, `issue.assignee_changed`, `issue.parent_changed`, `issue.project_changed`, `issue.labels_changed`, `issue.properties_changed`, `issue.metadata_changed` |
| Comment | `comment.created`, `comment.updated`, `comment.deleted`, `comment.resolved`, `comment.unresolved` |
| Reaction | `reaction.added`, `reaction.removed` (issue or comment) |
| Attachment | `attachment.attached`, `attachment.detached` (issue or comment) |

`task.started` means the persisted run entered `running`. Retries emit a new
`task.queued` with `retry_of_task_id`; manual reruns carry `rerun_of_task_id`.
Queue/defer transitions may happen repeatedly. Unchanged writes, bookkeeping
revisions, duplicate reactions, and pruning an already-deleted comment do not
produce another fact. Attachment events describe binding to an issue/comment,
not uploading an unbound file; moving a file produces detach and attach facts.

`issue.updated` includes `changed_fields` for meaningful issue fields, excluding
position, revision and timestamps. Specialized issue events can accompany it;
subscribing to both produces two inputs that the normal dispatcher coalesces.
Metadata/properties events include changed keys, never their values. Comment
events contain comment/thread/parent references, not bodies. Attachment events
contain references, not private URLs or filenames. Agents pull current state to
decide what to do. There is no business-condition evaluator.

Each newly captured fact includes `event_id`, `event_type`, `version`,
`occurred_at`, workspace/issue IDs, actor identity and optional source run/agent.
The already-terminal registration snapshot additionally has `observed_at` and
`registration_snapshot`; its `occurred_at` can be null for historical runs with
no completion timestamp. Older queued payloads remain readable.

`--filter-agent-id` matches the run's agent for run events and the actual source
agent for mutation events. Editing another agent's comment does not make that
agent the editor; payloads distinguish actor from author. `--task-id` accepts
only run events. Filters never expand the subscription beyond its current issue.

The platform lifecycle names `issue.created` and `issue.deleted` are listed
separately and rejected for self-wakeups: subscription requires an existing
issue, and deleting it withdraws its work. Workspace/cross-issue subscriptions,
external CI push events, and expanding the plugin subscription contract are
outside this version. Terminal issue transitions disable wakeups rather than
starting a final run on the closed issue.

## Implementation

- `pkg/eventcontract` owns stable business-event names independently of plugins.
  Plugin constants alias the existing names, preserving their payload contract.
- `issue_wakeup` stores configuration and `issue_wakeup_receipt` stores matching
  inputs. Run transitions and the collaboration changes in the catalog above
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
- Mutations and run events from the same wakeup's run are ignored. HTTP mutation
  transactions stamp server-resolved actor and source task identity in local
  PostgreSQL settings; those settings do not survive connection reuse. A client
  cannot choose source identity through a request body or an untrusted task header.
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

Migration 511 adds capture hooks without indexes, table rewrites, or foreign
keys. Deploy it before admitting subscriptions to the expanded catalog. Older
servers still dispatch the added receipts and older sidebars fall back to raw
event names; only updated servers accept create/update with new event types.
During a rolling upgrade, mutation attribution from older HTTP servers is best
effort (some older write paths provide no source identity). Keep new event
subscriptions disabled until all mutation-serving instances are upgraded, so
their own writes cannot feed back without attribution. Before rolling application
code back, disable subscriptions using the expanded catalog; before rolling the
capture migration back, drain their inputs as well. The down migration restores
the original five-event capture behavior and retains configuration/receipt data.

Migration 512 adds a concurrent partial index for enabled workspace summaries.
It can be rolled back independently of configuration data. Deploy the server
before the UI: an older server lacks the summary endpoint, so cards cannot
show future wakeups until it is upgraded. Existing task activity still works.
New detail fields are optional for rolling compatibility.

Migration 514 excludes the registering run's own events, in addition to events
from runs produced by the same rule. Human/external events with no source run
still match. Apply this migration before enabling broad agent-created subscriptions.
Rolling it back restores the previous capture function without rewriting data;
disable affected subscriptions first to avoid registration feedback.

This unmerged branch's migrations use prefixes 500–514 to follow main's 495–499.
Local databases that already applied the previous 495–508 wakeup filenames must
rename those exact `schema_migrations.version` entries by +5 before updating.
Do not rename main's migrations or rerun the table-creation migration. Fresh
databases use the normal migration runner.

Dispatch keeps the instruction and recent evidence within a 40,000-byte prompt
budget. Large or older details are explicitly condensed; original receipts remain
linked to the task and are consumed atomically with its queue update. A claimed
prompt is immutable, so further inputs remain pending until it starts or recovers.
After the normal 90-second claim recovery window with an expired/absent prepare
lease, `last_error` exposes that wait. Existing runtime claim recovery owns retries;
wakeups do not add another execution timeout. Timer progress advances while waiting.

Integration coverage includes transaction rollback, already-terminal registration,
source scope, one-shot deduplication, independent comment/assign input, merging,
self-loop suppression, custom terminal statuses, reopen behavior, offline timers,
configuration replacement, revoked permission at enqueue/claim, and retry prompt
inheritance. UI tests cover disabling and consumed one-shot state; API response
schemas reject malformed wakeup state rather than presenting an empty list.
Expanded-event integration tests execute writes for every advertised event and
cover attachment rebinding, repeated queue transitions, tombstone cleanup,
meaningful-change suppression, rollback, actor/author distinction, forged source
headers, metadata redaction and self-loop suppression on HTTP mutations.

## Workspace management

Web and Desktop expose **Issue wakeups** inside the Autopilot page (`?tab=wakeups`).
The tab remains available without any autopilots. `GET /api/issue-wakeups` returns
an access-filtered inventory with counts, agent filter choices, and offset
pagination (50 by default, maximum 100). Scope, trigger kind, target agent, and
literal case-insensitive search run on the server; page rows and counts share
one database snapshot. Prompts are omitted from this collection response.

Active means an enabled rule on an open issue **or** an unfinished run, including
consumed one-shot rules and manually disabled rules with running work. The query
looks up runs by their wakeup context, so a retry remains visible even if the
rule's last-task pointer refers to an older attempt. Configuration state and run
state are separate columns. Rules on terminal issues appear under Ended once
all their runs finish. Counts include only agents visible to the requester;
private source-agent and source-run references are redacted.

Trigger labels describe conditions (for example, "When Emacs's run succeeds"),
not an outcome that has already happened. The monitored agent and specific run
are distinct from the agent to wake. Multiple event types mean any of those events.
Rule labels distinguish waiting, scheduled, turned off, triggered, expired, and
stopped because the issue ended. A consumed one-shot is Triggered even when its
execution is still queued or has failed. A manually disabled rule remains Turned
off even if its previous execution succeeded. Execution results have their own
labels and transcript entrypoint. Familiar schedules use natural language, while
details retain the original cron and timezone. Disabled one-time rules retain
their original scheduled time.

The sidebar and inventory share enable, resubscribe, reschedule, and withdrawal
controls. Batch disable is limited to explicitly selected enabled rules on the
current page. It confirms that already-started runs continue, sends bounded
sequential calls to the existing authorized disable endpoint, and retains only
failed selections for retry. The inventory polls every ten seconds; mutations
invalidate inventory, sidebar, board summaries, and task caches. No scheduler or
Autopilot execution semantics change.
