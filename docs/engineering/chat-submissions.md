# Chat submission acceptance receipts

`POST /api/chat/sessions/{sessionId}/messages` accepts an optional UUID
`submission_id` alongside `content` and `attachment_ids`.

```json
{
  "content": "Review the deployment plan",
  "submission_id": "646fed2d-71b3-4cbb-bdb3-e39675f005c3",
  "attachment_ids": []
}
```

Choose a submission UUID when creating a logical send and persist it with the
request. After a timeout or lost response, retry that UUID with the same content
and requested attachments. A successful first send and its replays return
`201` with the original `message_id`, `task_id`, `created_at`, `queued`,
`supports_queue`, and actual bound `attachment_ids` snapshot. Replaying an
acceptance keeps the message, task, queue position, title work, events and
daemon wakeup at their original first-send counts.

Receipts are scoped to the workspace, authenticated user, server-resolved actor
type and ID, chat session, and submission UUID. Rotating a task token for the
same logical agent preserves that identity. Every replay requires current
workspace membership, session ownership/public projection, and agent view and
invocation access. Accepted requests can replay after the session or agent is
archived or its runtime becomes unavailable.

If invocation permission is revoked, replay returns `403` before reading the
receipt. Stop automatic retries and surface the permission failure to the user;
do not choose a new UUID to bypass it.

The fingerprint uses exact decoded content bytes and sorted distinct parsed
attachment UUIDs. Omitted, `null`, and empty attachment lists are equivalent.
The requested IDs define the fingerprint even when some attachments could not
be bound. Reusing an accepted UUID with a different fingerprint returns `409`
without changing the turn. An empty or malformed provided submission UUID
returns `400`.

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
queued-turn editing/removal, message deletion, and attachment deletion or
detachment preserve the receipt and original response. Replaying after these
operations keeps the original task ID and leaves the current message, task and
attachment state intact. Use a new UUID when deliberately sending a restored
draft or repeating identical text as a new turn. Read the task and chat APIs
for current execution state.

Receipts persist until their session is deleted. Session, workspace, and system
runtime-agent teardown remove them atomically under the existing session-lock
protocol. User-agent sessions surviving a runtime unbind keep their receipts.
Requests for a deleted session receive the existing `404` response.
The receipt's `created_at` records its creation time for diagnostics; it does
not introduce a TTL or change retention.

Omitting `submission_id` or setting it to `null` uses the existing send behavior.
Each such request creates a new turn. Clients can adopt the field independently.
Keyed writes require a transaction starter and commit the task, input ownership,
message, attachment bindings, initial title, session touch and receipt together.
Migrations 653–655 create the receipt table and concurrent indexes.
