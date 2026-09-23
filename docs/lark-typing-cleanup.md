# Lark Typing reaction cleanup

Before sending an Add request, the server records each input in
`channel_typing_reaction`, including the source message, session, workspace,
installation and encrypted credential snapshot. A failed registration prevents
the Add. Cleanup records survive deletion of the original session or installation.

Terminal processing enumerates every input in the shared ledger, including
coalesced inputs that are not the reply delivery's trigger. Cleanup ownership is
monotonic: an Add returning after another replica observes termination cannot
restore an active state. Failed flushes persist an input cutoff and context
revision before detached ingestion, so taskless settlement also works when it
precedes Add registration. Later inputs and rolled-back transactions do not
authorize cleanup of the wrong turn.

Known reaction IDs are deleted precisely. Both sweeps require the reaction's
operator type to be `app` and its operator ID to match the installation's App ID;
other apps and humans are preserved. An empty sweep cannot acknowledge an
unfinished Add before its retention deadline. Legacy unregistered reactions retain
the delivery-anchor fallback, which cannot reconstruct every historical input.

A taskless indicator expires two minutes after registration. This recovers the
badge when a hard crash loses an in-memory debounce timer after Add succeeds.
It does not settle the input, cancel a task, or autonomously restart a lost run;
the channel engine can process that input on subsequent traffic. A long burst
can therefore lose its oldest taskless indicators before flushing. Inputs with
active tasks and newer inputs keep their own indicators, within the seven-day
maximum lifetime below.

## Retry and retention limits

- The worker runs every 30 seconds, claims at most 100 rows using `SKIP LOCKED`,
  and applies a 30-second to one-hour backoff. Its pass has a ten-second budget;
  each remote operation has a two-second budget. It does not hold database locks
  across remote calls.
- Claims advance retry counters before remote execution. A claimed row that does
  not fit the pass budget is deferred too; the counter measures claims, not HTTP
  attempts. Backlog latency and throughput are not guaranteed.
- All indicators have a seven-day maximum lifetime from registration, including
  running tasks, uncertain Adds and permanent credential failures. No new remote
  retries are claimed after that deadline. An in-flight call has at most a
  two-second budget. A late Add result cannot resurrect an expired ledger row.
- Confirmed cleanup erases its encrypted snapshot immediately. Expiry erases the
  snapshot and records `abandoned_at`, leaving `cleaned_at` null: this is an
  explicit failed terminal outcome, not a claim that the remote badge disappeared.
  A deleted workspace retains credentials only within the same remaining window;
  it does not restart the retention clock. Revoked credentials, permanently failed
  APIs or arbitrarily late remote writes can leave a badge requiring manual removal.
- Expiration and pruning each get an independent two-second database budget before
  remote retries, every 30 seconds. Each handles at most 1,000 rows with
  `SKIP LOCKED`. Non-secret success/failure tombstones are pruned seven days after
  their terminal timestamp. Physical redaction requires a healthy running worker
  and database: downtime, locks or maintenance backlog can delay it beyond the
  logical deadline. Database backups follow the operator's separate retention
  policy. Do not describe this as a guaranteed wall-clock erasure SLA.
- Each workspace has at most 1,000 pending ledger slots. A unique partial index
  arbitrates concurrent registration; a full quota or a slot collision skips the
  cosmetic Add without blocking input processing. Migration abandons/redacts any
  existing pending rows beyond that quota. Active tasks themselves are unaffected.
  Operators should treat repeated quota skips as a capacity incident, not normal
  successful processing. Increasing this cap requires an explicit capacity review.

Expiry emits a warning with workspace ID and count; maintenance failures emit an
error; quota/contention skips and failed remote withdrawals emit warnings without
credentials. Alert on these events and on an increasing overdue-redaction count.
The following read-only query reports the durable operational state without
exposing snapshots:

```sql
SELECT workspace_id,
 count(*) FILTER (WHERE cleaned_at IS NULL AND abandoned_at IS NULL) AS pending,
 count(*) FILTER (WHERE cleaned_at IS NULL AND abandoned_at IS NULL
   AND created_at <= now() - interval '7 days') AS overdue_redaction,
 count(*) FILTER (WHERE abandoned_at IS NOT NULL) AS abandoned,
 min(created_at) FILTER (WHERE cleaned_at IS NULL AND abandoned_at IS NULL) AS oldest_pending
FROM channel_typing_reaction GROUP BY workspace_id;
```

Remote retry claims still advance the backoff before execution; a 100-row batch
can exceed the ten-second pass budget. Attempt counters count claims, not HTTP
calls, and retry throughput is not guaranteed. This cannot starve the independent
credential/GC passes, but requires workload-based capacity validation before rollout.

## Migrations and rollback

Migration 536 adds the ledger and taskless settlement flag. Migrations 537–539
build the three indexes concurrently and register invalid-index cleanup hooks.
Migration 540 adds the retention outcome and quota slots; 541–543 build quota,
expiry and abandoned-record indexes with the same interrupted-build recovery.
Interrupted builds are dropped and rebuilt before being marked applied; valid
indexes are preserved on retry. There are no new foreign keys or cascading deletes.

Validate upgrades using the complete historical schema, retained input rows,
repeated `up`, and interrupted-index recovery. Four migrations against a minimal
table fixture alone do not establish full upgrade compatibility.

Application rollback preserves the schema, ledger and encryption keys. An older
server may not enforce the new quota/expiry policy. Keep a compatible cleanup
worker running or perform an explicitly reviewed offline redaction before retiring
it; image rollback alone does not preserve these new retention guarantees.
Bare `migrate down` rolls back all
applied migrations in the directory, not just these four, and must not be used as
an application rollback shortcut. Review pending cleanup records before any
separately planned schema removal.
