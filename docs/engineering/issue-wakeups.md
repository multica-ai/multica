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
three previews per issue, never prompts or history. Access follows shared issue visibility and workspace membership; private
source-agent and source-run filters are redacted consistently. The issue surface polls once every ten
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

`--filter-agent-id` selects the run's agent for task events. New mutation-only
requests using this legacy flag normalize to `filter_actor_type=agent` and
`filter_actor_id`; use actor flags for comment/issue/reaction/attachment changes.
Existing stored rules and legacy mixed task/mutation requests remain supported. Editing another agent's comment does not make that
agent the editor; payloads distinguish actor from author. `--task-id` accepts
only run events. Filters never expand the subscription beyond its current issue.

Use `--filter-actor-type member --filter-actor-id USER_ID` to wait for a
specific workspace member, or type `agent` for a source agent. For example:

```sh
multica issue wakeup create ISSUE --kind event --event comment.created \
  --filter-actor-type member --filter-actor-id USER_ID \
  --instruction-file ./instruction.md
```

These optional paired filters apply to issue, comment, reaction and attachment
changes. They match the actor who performed the change; for a new comment this
is its author, but for an edit it is the editor, not the original author. Unknown
system attribution does not match. Task subscriptions keep their existing
agent/run filters. Actor filters cannot be combined with those source filters.
The ID is the user's UUID, not the membership record's UUID; it must belong to
the current workspace. Other people's events neither enqueue nor consume a
one-shot rule. Broad subscriptions without actor filters behave as before.
Enable/rearm retains the stored actor filter; an explicit full update can replace
or clear it. The sidebar, board summary and inventory show the actor's name;
private agent references are redacted, retaining a generic selected-actor label.

Migrations 531–532 add nullable actor columns and update transactional capture.
Apply migrations, then deploy the API/CLI before creating actor-filtered rules;
update web/Desktop to display the actor restriction. Old clients can read the
additive response, but cannot show the new restriction. On application rollback,
keep these migrations: the database still enforces stored filters even for older
consumers. Schema rollback deliberately refuses while any actor-filtered rule
exists, including disabled rules, so a later re-enable cannot silently broaden
it. Disable/drain and remove those configurations before rolling 532/531 back.

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
- Event notifications merge by rule, revision and event type, keeping the first
  occurrence time, count and latest source reference. Agents read current state
  and comment history when intermediate references have been condensed.
  Time inputs replace the pending time note with the newest signal. A claimed
  prompt is immutable; later input becomes at most one subsequent queued task.
- Mutations and run events from the same wakeup's run are ignored. HTTP mutation
  transactions stamp server-resolved actor and source task identity in local
  PostgreSQL settings; those settings do not survive connection reuse. A client
  cannot choose source identity through a request body or an untrusted task header.
- `context.wakeup_id` and `context.wakeup_revision` identify the new trigger.
  Bounded structured facts live in additive `context.wakeup_evidence` (version 1).
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

Migration 520 adds capture hooks without indexes, table rewrites, or foreign
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

Migration 521 adds a concurrent partial index for enabled workspace summaries.
It can be rolled back independently of configuration data. Deploy the server
before the UI: an older server lacks the summary endpoint, so cards cannot
show future wakeups until it is upgraded. Existing task activity still works.
New detail fields are optional for rolling compatibility.

Migration 523 excludes the registering run's own events, in addition to events
from runs produced by the same rule. Human/external events with no source run
still match. Apply this migration before enabling broad agent-created subscriptions.
Rolling it back restores the previous capture function without rewriting data;
disable affected subscriptions first to avoid registration feedback.

This unmerged branch's wakeup migrations use prefixes 509–532, following
main's migrations through 508. The previously used 500–523 wakeup names were
renumbered by +9 without changing their SQL or relative execution order.

Before updating a local database that has already applied the old branch,
stop its API and daemon and rename only the exact wakeup
`schema_migrations.version` entries from the old filenames to the new filenames
(+9). Do not rename main's similarly numbered migrations or rerun the base
wakeup table creation. Databases still on the older 495–508 wakeup names first
need the previous +5 rename to the 500–513 names, then this +9 rename.
Keep this ledger change transactional; leave all wakeup rules, receipts and
queued tasks intact. Fresh databases use the normal migration runner.


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
all their runs finish. Counts include every rule on shared issues, including rules targeting private
agents. Reading a rule does not grant permission to invoke or configure its
agent. Private source-agent and source-run references are redacted in inventory,
sidebar and board summary responses alike. Instructions remain shared issue
content and are omitted from inventory and summary responses.

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


## Database load and notification retention

The scheduler discovers candidates from due timers and the partial pending-receipt
index, instead of scanning disabled rule history. Each dispatch has a two-second
budget and a 50 ms PostgreSQL lock timeout; diagnostic/fairness writes have their
own 100 ms budgets. Contention leaves inputs pending for the next tick and does
not spend the full batch deadline on one issue. The global inventory counts only
unfinished runs before pagination; indexed latest-run lookups happen for the
selected page, rather than sorting the entire completed-run history.

At most 32 rules per issue and 1,000 per workspace can be enabled. These are
activation limits: disabled history does not count, existing rules are not
silently removed, and editing an already-enabled rule does not consume another
slot. A database trigger serializes activation within a workspace, including
concurrent creation on different issues. Capacity errors return HTTP 400 with
`wakeup_capacity_exceeded`. These initial limits bound synchronous source-event
fanout and can be revisited with measured workload data.

New event captures retain at most one pending notification per rule revision
and event type (25 currently supported types). Repeated inputs retain a count,
first occurrence time and the latest source reference, with an explicit prompt
instruction to read source state. This is a wakeup notification, not an immutable
event archive or a promise to execute once per source event. Legacy pending
receipts remain readable and drain in batches of 100. Processed receipts become
eligible for deletion after seven days, with up to 1,000 deleted per scheduler
tick; backlog can extend that retention. Pending inputs are never age-expired.
Run history and source comments are unaffected. Receipt keys suppress retained
first/latest fact duplicates; they are not a permanent deduplication ledger for
all intermediate coalesced facts.

Migrations 524–527 add concurrent lookup/expiry indexes, 528 adds a nullable
coalescing key, 529 adds its partial unique index, and 530 enables bounded capture
and activation limits. Apply in that order before the new server. No existing
receipt rewrite is needed. New consumers lock receipt rows before constructing
queue evidence. Each merge also rotates its receipt ID: an old consumer can only
mark the version it read as processed, so a concurrent replacement remains
pending during rolling upgrades (a redundant check is possible, lost new input
is avoided). Existing event/task keys and API fields remain compatible.

Rolling application code back leaves the database limits and coalescing active;
old consumers can still drain the notifications. To roll the schema back, reverse
530 before dropping 529/528 and the additive indexes. This restores the prior
capture function and removes limits without deleting rules or pending inputs.
The base wakeup tables must still be retained while any wakeup runs reference them.

## Summary compatibility and capture locks

Pending-run evidence is merged as structured facts and rendered once, without
parsing a previous prompt's headings or omission messages. The total rendered
note still fits 40KB. Older tasks, or notes rewritten by an older dispatcher,
are carried as one opaque bounded legacy item; overflowing history is marked
as condensed and the latest input remains readable. Existing readers and retries
continue using `handoff_note`; no migration or new execution lifecycle is needed.

Dispatch takes the pending-task lock before locking event receipts, so waiting
for an existing task does not hold up capture on those receipt rows. Receipt
consumption and task enqueue/update still commit in one transaction. Source
writes can still wait during that short critical section; this is not a promise
of nonblocking comment writes. Splitting enqueue from consumption would require
another delivery/recovery protocol and is intentionally avoided.

Direct self-trigger protection is scoped to one rule and trusted source-run
identity. Cross-rule cycles remain possible; avoid mutually triggering continuous
comment subscriptions. Prefer a member actor filter for waiting on a human.
Target agent names follow shared issue visibility, while source references and
invocation/management authority keep their existing checks.


### Editing wakeup instructions

The issue sidebar details and Autopilot wakeup list share a prompt editor. The
workspace list loads the issue's prompts only when the editor opens. Saving uses
`PATCH /api/issues/:id/wakeups/:wakeupID/instruction` with `instruction`,
`expected_instruction`, and `revision`; success returns 204. The existing full
replacement PUT still has its original reconfiguration/rearm semantics.

Prompt-only edits preserve schedule, enabled/consumed state, creator, provenance,
subscription revision, pending receipts and existing runs. Later dispatch uses the
new instructions (including when merging new evidence into a still-pending run);
saving itself does not rewrite queued or running tasks. Structured evidence keeps
the instruction used for its last rendering, so editing does not turn structured
facts into a nested old prompt. Pre-snapshot notes remain bounded historical
context, explicitly subordinate to the current instruction. Closed/disabled rules may
be edited without resuming them. The caller must be the creator or a workspace
admin/owner and must currently be allowed to invoke the target agent.

Under the issue/config locks, both the prior prompt and subscription revision
must match; otherwise saving returns 409 and the UI retains the draft. Keeping
the subscription revision avoids invalidating receipts and queued work on a text
edit. This additive endpoint needs no migration and does not change existing
clients. Deploy the API before using the editor; an older API rejects the new
endpoint and the editor keeps the unsaved text.
