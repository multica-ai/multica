# Chat submissions

`POST /api/chat/sessions/{sessionId}/messages` accepts `content`, an optional
UUID `submission_id`, optional `attachment_ids`, and an optional boolean
`steer`.

```json
{
  "content": "Review the deployment plan",
  "submission_id": "646fed2d-71b3-4cbb-bdb3-e39675f005c3",
  "attachment_ids": [],
  "steer": true
}
```

This document covers the acceptance receipt for `submission_id` and the
lifecycle of a send accepted into a running reply (steering). The product
behavior is described in `apps/docs/content/docs/chat.mdx` ("Send messages
while a reply is in progress"). Issue-comment steering uses the same daemon
transport; its server side is `server/internal/handler/task_supplement.go`.

## Send mode

`steer` is opt-in. Only an explicit `true` asks the server to deliver the
message into the session's running reply. Omitted, `null` and `false` keep the
queue behavior that existed before steering: a new turn is created and, when a
reply is active, `queued` is `true`. Installed desktop builds, mobile, and
the internal `ResumeAfterDecision` send (`connected_apps.go`) do not send the field, so their
**Queue message** semantics do not change. Current web and desktop clients send
`steer: true` from **Send** and `steer: false` from **Queue message**
(`packages/core/api/client.ts`, `sendChatMessage`).

`steer: true` is a request, not a guarantee. The server steers only when all of
these hold inside the send transaction (`sendDirectChatMessage` in
`server/internal/service/task.go`, `LockRunningChatTaskForSupplement` in
`server/pkg/db/queries/chat_supplement.sql`):

- the server-resolved actor is a member, not an agent token;
- the request has no attachments;
- the session has a `running` turn that is not regenerating quick actions;
- that turn's daemon negotiated `task-supplement-v1` at start. The daemon
  advertises it only when `SupportsTaskSupplement` in
  `server/pkg/agent/version.go` accepts the provider and version, so older
  daemons and unsupported providers never qualify;
- the session is not bound to a Slack or Lark channel.

Otherwise the request falls back to an ordinary queued send in the same
transaction and the response carries `steered: false`. Clients must not treat
that as an error.

## Response fields

A successful first send and its replays return `201` with `message_id`,
`task_id`, `created_at`, `queued`, `supports_queue`, `steered`,
`message_task_id`, `message_created_at`, and the bound `attachment_ids`
snapshot. The three steering fields are optional: older servers omit them, and
`steered` is omitted when `false`.

| Field | Queued send | Steered send |
|---|---|---|
| `task_id` | The new turn | The running attempt that will receive the message |
| `created_at` | That turn's creation time | The running attempt's creation time, earlier than the message |
| `message_task_id` | Same as `task_id` | The root input owner of the running attempt (`chat_input_task_id`, which differs from `task_id` during an automatic retry) |
| `message_created_at` | The user message's creation time | The user message's creation time |
| `queued` | `true` when a reply was active | `false` |
| `steered` | `false` (omitted) | `true` |

`task_id` names the run. `message_task_id` names the input batch the message
belongs to, which is what `chat_message.task_id` stores and what the
`chat:message` event carries. Place the optimistic user message with
`message_task_id ?? task_id` and `message_created_at ?? created_at`; using the
task time would sort a steered message above earlier replies. A steered send
creates no turn, so clients must not seed a pending task for it
(`use-chat-controller.ts`).

`201` with `steered: true` means the server durably accepted the message for
delivery. It does not mean the agent has read it. Delivery is confirmed later
by the daemon and is not reported back to the sender; the observable outcomes
are the reply continuing or, when delivery does not happen, a new queued turn
announced by `task:queued`.

## Acceptance receipts

Choose a submission UUID when creating a logical send and persist it with the
request. After a timeout or lost response, retry that UUID with the same
content, requested attachments and send mode. Replaying an acceptance returns
the original response and keeps the message, task, queue position, steering
binding, title work, events and daemon wakeup at their original first-send
counts.

Receipts are scoped to the workspace, authenticated user, server-resolved actor
type and ID, chat session, and submission UUID. Rotating a task token for the
same logical agent preserves that identity. Every replay requires current
workspace membership, session ownership/public projection, and agent view and
invocation access. Accepted requests can replay after the session or agent is
archived or its runtime becomes unavailable.

If invocation permission is revoked, replay returns `403` before reading the
receipt. Stop automatic retries and surface the permission failure to the user;
do not choose a new UUID to bypass it.

The fingerprint uses exact decoded content bytes, sorted distinct parsed
attachment UUIDs, and the requested send mode. Omitted, `null`, and empty
attachment lists are equivalent. Omitted, `null` and `false` `steer` are
equivalent and hash exactly as receipts written before steering existed. The
requested IDs and the requested mode define the fingerprint even when some
attachments could not be bound or a `steer: true` request fell back to the
queue. Reusing an accepted UUID with a different fingerprint, including a
different `steer` value, returns `409` without changing the turn. An empty or
malformed provided submission UUID returns `400`.

The endpoint also uses `409` for an archived agent, an unavailable runtime, or a
session archived after preflight. Distinguish a submission conflict by the JSON
`error` value `submission_id was already accepted with a different payload`;
status `409` alone does not identify it. Stop retrying that UUID with the changed
payload; use the original persisted request to recover its acceptance. An empty
`content` with an already accepted UUID returns `409` because its fingerprint
differs. With a new UUID it returns `400` (`content is required`). A session
already archived at handler preflight returns the existing `400` (`chat session
is archived`); the transaction's archive-race check returns `409`.

An acceptance receipt records the historical accepted send. Cancellation,
queued-turn editing/removal, message deletion, attachment deletion or
detachment, and any later requeue of steered guidance preserve the receipt and
original response. Replaying after these operations keeps the original task ID
and leaves the current message, task and attachment state intact. Use a new
UUID when deliberately sending a restored draft or repeating identical text as
a new turn. Read the task and chat APIs for current execution state.

Receipts persist until their session is deleted. Session, workspace, and system
runtime-agent teardown remove them atomically under the existing session-lock
protocol. User-agent sessions surviving a runtime unbind keep their receipts.
Requests for a deleted session receive the existing `404` response.
The receipt's `created_at` records its creation time for diagnostics; it does
not introduce a TTL or change retention.

Omitting `submission_id` or setting it to `null` uses the existing send behavior.
Each such request creates a new turn or a new steering binding. Clients can
adopt the field independently. Keyed writes require a transaction starter and
commit the task or steering binding, input ownership, message, attachment
bindings, initial title, session touch and receipt together. Migrations 653–655
create the receipt table and concurrent indexes.

## Steering lifecycle

A steered send writes the user message and one `chat_task_supplement` row in
the send transaction (migrations 661–664; queries in
`server/pkg/db/queries/chat_supplement.sql`). Two owners are recorded on
purpose:

- `chat_message.task_id` is the root input owner (`chatInputOwnerID`). Retries
  inherit `chat_input_task_id`, so a retry reads the guidance as ordinary input.
- `chat_task_supplement.task_id` is the exact attempt the guidance was offered
  to. It is a delivery attempt record and never decides what a retry reads.

Row states:

| Status | Meaning |
|---|---|
| `pending` | Accepted, not yet claimed by the daemon |
| `delivering` | Claimed; the injection result is not yet known |
| `delivered` | The daemon acknowledged a successful injection |
| `requeued` | Moved to a new queued turn; the message now belongs to that turn |
| `withdrawn` | Retired without a new turn: kept for a retry, or the run's access was revoked |

Writers lock the chat session, then the task row, then supplement rows. This
matches the completion path, so a send, an acknowledgement and a terminal
transition serialize on the task row.

### Delivery

After commit the handler sends a content-free wakeup to the daemon. The
daemon's `runTaskSupplementLoop` (`server/internal/daemon/task_supplement.go`)
claims one row at a time through `POST /api/daemon/tasks/{taskId}/supplements/claim`
and acknowledges it through `.../supplements/{commentId}/ack`. For chat tasks
the wire `comment_id` carries the chat message ID, so the daemon protocol is
unchanged. Claiming moves `pending` to `delivering` in creation order and only
while the task is `running`.

- `delivered: true` sets `delivered`. Repeating it is a no-op. When the row was
  already requeued or withdrawn, the ack returns `409` and changes nothing.
- `delivered: false` requeues only that message. Repeating it is a no-op.

The daemon stops and joins this loop before it reports the task's terminal
state or acknowledges a cancellation, so in the normal path every claimed row
has its result recorded before the turn ends.

### Turn end

Terminal transitions run `settleTerminalTaskState` inside their transaction:

- **Completed, or failed without an automatic retry:** `pending` and
  `delivering` rows are requeued. Each becomes a new queued turn owned by the
  message's author; the message moves to that turn.
- **Failed with an automatic retry:** `FailTaskWithTransition` creates the
  retry first, then marks the attempt's open rows `withdrawn`. The message
  stays in the root input batch, so the retry reads it after the original
  request and under the retry's backoff. No separate turn can run ahead of
  the retry. Daemon recovery and infrastructure sweeps (`terminateTasksInTx`)
  apply the same rule using `retryEligible`, the predicate that schedules
  their retry after the transaction.
- **Stopped by the user:** when any row is `delivering`, Stop requeues nothing.
  It sets `chat_finalize_deferred_at` and also holds `pending` rows, so later
  guidance cannot overtake the in-flight one. The pending ack then settles that
  row (delivered stays delivered, rejected is requeued alone). The daemon's
  `cancel-ack`, or the sweeper after `chatFinalizeGraceSeconds` (60 s,
  `server/cmd/server/runtime_sweeper.go`), calls
  `FinalizeDeferredCancelledChat`, which requeues what is still open. Both the
  synchronous and deferred Stop paths check `HasChatTaskAssistantOutcome`, so
  the "Stopped" outcome is written at most once.
- **Draft restore on Stop** moves only user messages without a supplement row
  back to the composer. Guidance whose row is not `requeued` stays in the
  conversation (`DeleteUserChatMessageByTask` in `chat.sql`).

Requeued turns are published with `task:queued` and a daemon wakeup only after
their transaction commits (`publishRequeuedChatTasks`, and the ack handler
after its own commit). Do not publish from inside the transaction.

### Withdrawal and deletion

Revoking access must not start new work. These paths mark open rows
`withdrawn` before settling the cancelled tasks: member removal
(`revokeAndRemoveMember` in `workspace_revoke.go`), runtime access revocation
(`PrepareRuntimeAccessCancellation`), runtime teardown (`TeardownRuntime`),
stopping all of an agent's work (`CancelTasksForAgent`) and archiving an agent
(`CancelTasksForArchivedAgent`). A late delivery ack for a withdrawn row returns
`409`. Session deletion removes the session's supplement rows and capability
rows; workspace deletion removes the workspace's rows.

### Resumed provider sessions

The claim for an automatic retry still carries the full input in
`chat_message`. When the retry resumes a prior provider session
(`RetryOfTaskID` and `PriorSessionID` set), the server also sends
`chat_resume_message`: the same input without guidance that was `delivered` to
an attempt with the same `session_id` and `runtime_id`
(`ListChatGuidanceInProviderSession`, `buildClaimedTaskResponse` in
`server/internal/handler/daemon.go`). The daemon uses it only while it still
holds that prior session ID (`buildChatPrompt`). A fresh session, or a fallback
after a failed resume (which clears `PriorSessionID`), uses the full input.
Daemons without the field send the full input.

### Delivery guarantee

When a delivery result is unknown, Chat treats the guidance as undelivered and
requeues it. The guarantee is at least once: a message is not lost, and it can
be processed twice. Known duplicate cases:

- Stop while a row is `delivering`, and the daemon never acks within the grace
  period;
- the daemon injected the message but its ack was lost before the turn ended;
- the provider returned an error after it had already accepted the message;
- an older daemon resumes a provider session and sends the full input.

Server state cannot undo what the agent did after reading guidance, so the
contract never promises exactly-once side effects outside Multica. Changes to
send, ack, stop, retry or teardown paths must keep this direction: when unsure,
requeue rather than drop.

## Regression tests

Run these when changing send modes, the response shape, fingerprints, delivery,
or any terminal path. Extend them instead of adding a parallel harness or
per-provider copies; provider injection itself is covered by the existing
`server/pkg/agent/*_supplement*_test.go` suites.

| Contract | Tests |
|---|---|
| Steered send, claim, idempotent ack, no extra turn | `TestChatSupplementSendClaimAck` in `server/internal/handler/chat_supplement_test.go` |
| Explicit opt-in, old daemon and legacy client stay queued | `TestChatSupplementQueueChoiceAndOldDaemon`, `TestChatSupplementLegacySendRemainsQueued` |
| Fallback on completion, failure, cancellation and provider rejection | `TestChatSupplementFallbacks`, `TestChatSupplementExhaustedRetryQueuesGuidance` |
| `task:queued` after commit | `TestChatSupplementTerminalFallbackBroadcastsQueued` |
| Stop waits for the delivery result, grace fallback, single outcome | `TestChatSupplementStopWaitsForDeliveryResult`, `TestChatSupplementStopGraceRequeuesUnconfirmedGuidance`, `TestChatSupplementStopWithTranscriptSettlesDeliveryWithoutDuplicateOutcome`, `TestChatSupplementDeliveredGuidanceSurvivesDraftRestore` |
| Retry keeps input ownership and order | `TestChatSupplementRetryKeepsInputAndOrder`, `TestChatSupplementDaemonRecoveryRetainsRetryInput` |
| Resume deduplication | `TestChatSupplementRetryClaimKeepsColdInputAndOmitsResumedGuidance`; daemon side `TestChatSupplementRetryPromptUsesResumeInputOnlyWithPriorSession` in `server/internal/daemon/task_supplement_test.go` |
| Withdrawal and deletion | `TestChatSupplementAccessRevocationWithdrawsGuidance`, `TestChatSupplementBulkStopDoesNotStartNewWork`, `TestChatSupplementRuntimeTeardownDoesNotStartNewWork`, `TestChatSupplementPermissionAndSessionDeletion` |
| Send mode in the fingerprint | `TestChatSupplementSubmissionReplayPreservesSendMode` |
| Receipt identity, replay, conflicts, teardown | `server/cmd/server/chat_submission_test.go`, `chat_submission_contract_test.go`, `chat_submission_race_test.go`, `chat_submission_teardown_test.go` |
| Client request and response parsing | `sendChatMessage explicitly opts out of steering for Queue message` in `packages/core/api/client.test.ts`; `preserves steering acceptance and tolerates older or malformed metadata` in `packages/core/api/schemas.test.ts` |
| Composer and optimistic placement | `offers default Send and an explicit queue action while replying` and `sends guidance by default during a reply without forcing the queue` in `packages/views/chat/components/chat-input.test.tsx`; `keeps the active retry and stores guidance under its root input owner` in `packages/views/chat/components/use-chat-controller.test.tsx` |
