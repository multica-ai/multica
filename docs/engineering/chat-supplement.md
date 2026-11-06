# Direct Chat guidance for an active Codex run

Ordinary sends remain queued. **Add to current run** explicitly sends text to
one running task whose daemon and server negotiated Supplement support. Both
the full Chat page and floating Chat window use the same composer and mutation.
Attachments keep using the ordinary send path.

## Delivery contract

The server rechecks Chat ownership, workspace, agent invocation permission and
the exact task. A single SQL statement locks the session and then the task,
creates the historical `chat_message` and its `chat_supplement` receipt, and
touches the conversation. Completion winning the race leaves no new message.
It never substitutes the next task or creates a second invocation. The task,
author and client request UUID provide idempotency for explicit retries.

The existing daemon claim/ack routes and `Session.Supplement` transport are
reused. Their legacy wire field `comment_id` holds the opaque chat message ID
for a Chat task. Codex uses `turn/steer` with `expectedTurnId`; an RPC rejection
and a transport loss are distinguished. Neither the daemon nor the client
silently resubmits an attempted input.

| UI state | Meaning |
| --- | --- |
| Waiting to send to current run | Persisted; not claimed by the daemon. |
| Sending to current run | Claimed by the daemon; waiting for engine acknowledgement. |
| Accepted by engine | Codex acknowledged `turn/steer`; this does not prove model compliance. |
| Not delivered | The turn ended before claim, was not started, or the engine explicitly rejected the request. |
| Delivery uncertain | An explicit acknowledgement reports an ambiguous outcome, or the task ended after claim without a positive acknowledgement. |

A claimed receipt is never claimable again, including after reconnection. A
late positive acknowledgement can resolve an uncertain terminal receipt.
Pending receipts become failed when their task ends; claimed receipts become
unknown. Users can copy failed/uncertain text and explicitly choose a subsequent
send. There is no automatic fallback to a queued task. Rejected HTTP submissions
keep the draft, including newer edits or a draft opened in another session.

Historical guidance remains visible after cancellation but is excluded from
initial input batches, legacy trailing input and cancellation draft restoration.
Session deletion, workspace teardown and runtime teardown remove receipts in
application code. Runtime teardown locks the system agents’ sessions and removes
receipts in the same transaction before deleting those agents. Detached user
agents retain their sessions and receipts. The shared helper covers manual
deletion, profile deletion and offline retention GC.

## Explicit retries after an uncertain HTTP result

Before submitting, the client persists the request UUID, session, exact task and
text using the existing workspace-scoped draft factory and StorageAdapter. These
are unconfirmed user submissions, not a second cache of server receipts. They
survive closing the window, navigation, switching between the main and floating
chat, and reloading. Logout and workspace deletion use the existing draft cleanup.

Retrying the same text uses the original UUID and task, even after that task has
ended or a successor has started. The server can return the existing receipt
before testing current availability. A different text gets its own request;
it cannot overwrite an unresolved one. Success removes only that request, and
the composer’s existing snapshot guard preserves newer edits and other sessions.
A successful response retains the originating workspace slug for cleanup: it
removes only that UUID from the latest persisted draft in that workspace, even
when another workspace is active. Missing storage after logout or workspace
deletion is never recreated; an ambiguous failure leaves the original identity
intact. The store also registers with cold-start draft cleanup before Chat mounts.
No request is sent on mount, rehydration or reconnect.

The retry action is labelled **Retry original request**. **Treat as a new
instruction (may duplicate)** explicitly forgets that request’s local retry
identity, without sending anything or changing the draft. A subsequent explicit
send creates a new identity. After a confirmed successful response, an identical
instruction also counts as a new instruction. Losing the browser’s local storage
loses unresolved identities; it cannot provide recovery across devices.

The error says the addition could not be confirmed, rather than claiming it was
not delivered. The claim commits before emitting the existing targeted
`chat:supplement` event. Both history queries are invalidated, so a previously
rendered pending receipt becomes sending without a page reload while the task
continues. A lost acknowledgement leaves it sending until terminal settlement
marks it uncertain. A late positive acknowledgement refreshes it to accepted.

## Storage and compatibility

A dedicated receipt table keeps Chat messages separate from issue comments.
Generalizing `task_supplement` itself would require changes to issue comment
replay, deletion, coverage and settlement paths. Only the capability table's
`issue_id` becomes nullable; `agent_task_queue` and `chat_message` retain their
existing column layouts. There are three migrations: table/nullability, request
uniqueness index and message uniqueness index. Both indexes build concurrently
in separate migration files. There are no new foreign keys or triggers.

Only direct Codex Chat tasks are newly negotiable; issue-task negotiation and
receipt behavior remain unchanged. Old daemons do not offer the capability;
old servers do not echo it. Missing or malformed UI flags hide the action.
Older clients ignore the additive receipt fields and continue queuing sends.
The down migration removes Chat receipts and Chat capability rows before
restoring non-null issue IDs. Rollback is lossy for receipt metadata, so do not
roll back while a Chat delivery is active.

Hermes and other engines are deliberately outside this implementation. No
interactive session persists beyond the existing task lifecycle.
