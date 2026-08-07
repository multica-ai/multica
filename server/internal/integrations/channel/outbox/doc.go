// Package outbox is the durable outbound delivery path for channel adapters
// that reach their platform over a long-lived connection.
//
// # The problem
//
// A hold-connection channel (WeCom aibot, Lark's WS mode, Slack socket mode)
// can only write to its platform from the replica currently holding that
// installation's connection lease. But the thing to write — an agent's reply,
// a task-failed notice — is produced by whichever replica happened to run the
// task. An in-process handoff between the two therefore only works when both
// are the same replica: on a multi-replica deployment every reply produced
// off-lease is silently dropped, and the only mitigation is to document that
// the deployment must run a single replica.
//
// # The shape
//
// This package replaces that in-process handoff with a table. Producers
// enqueue from any replica; the lease holder claims and drains. Concretely:
//
//   - [Producer] inserts rows, keyed by (source_kind, source_id) so the same
//     business result cannot be enqueued twice.
//   - [Consumer] runs on the lease holder for one installation: claims a due
//     row under a lease, re-checks that the target is still deliverable, hands
//     the row to the channel's [Sender], then settles it — sent, retried with
//     backoff, or dead-lettered past [MaxAttempts].
//   - [Reconciler] is the safety net for a producer that died between
//     finishing the task and inserting the row. It re-scans a trailing window
//     of terminal tasks and enqueues whatever is missing, so a crash costs
//     latency rather than a lost reply.
//   - [WakeRegistry] lets a producer nudge a local consumer so the common case
//     does not wait for the poll tick.
//
// # What is not here
//
// Nothing in this package knows a platform's wire format, credentials, or
// error codes. payload is an opaque JSONB document that the owning adapter
// renders at send time behind [Sender]; retry-vs-terminal classification is
// the adapter's call, expressed as a [Disposition]. That is what keeps the
// queue shareable: the tables are keyed on channel_type, and adopting this
// path costs an adapter one [Sender] implementation plus a [PayloadBuilder].
package outbox
