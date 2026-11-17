# Inbox read protection

Inbox list, unread count, account summary, snoozed list, legacy archive,
archive pages and archive facets share one execution budget per API process.
Admission happens before acquiring a primary database connection. Mutations and
daemon endpoints retain their existing behavior. No SQL or schema changes are
required, and old clients receive the same successful response shapes.

Defaults are two executions, twenty queued reads and 200 ms maximum admission
wait. Pool acquisition plus transaction setup has a separate 200 ms deadline.
Every expensive query runs in a read-only transaction with a transaction-local
3 s PostgreSQL statement timeout. Read work has a 5 s deadline; rollback has a
fresh context bounded to 250 ms and runs before releasing execution capacity.
Once SQL begins, HTTP cancellation does not release its permit early: the SQL
finishes or PostgreSQL stops it before the detached Go query deadline. Transport
failures retain capacity through the possible remaining server query budget.

Concurrent summary requests for the same authenticated account share one
execution using singleflight. There is no result cache. Shared execution has an
independent deadline so a canceled tab does not cancel another tab's request.
If a caller joins after a read starts, the shared execution reads again before
publishing, within the same total deadline and admission budget. This prevents
an event after a mutation from adopting the old query snapshot as fresh data.
Summary callers, including canceled callers retained until completion, are
bounded to execution concurrency plus twenty per process.

Capacity exhaustion, connection acquisition deadlines and database statement
timeouts return `503` with `Retry-After: 1`. Other database errors remain `500`.
Web and desktop forward cancellation for list, snoozed and summary HTTP reads.
These queries retain cached data on a failed refresh and skip automatic retries
on 503; a subsequent inbox event or explicit refresh can fetch again. Existing
archive queries already disable retries. Older clients may still retry, but
server execution and waiting remain bounded.

## Configuration and monitoring

All settings are optional. Invalid values log a warning and use the default.

| Environment variable | Default | Accepted values |
| --- | --- | --- |
| `INBOX_READ_CONCURRENCY` | 2 | 1–20 per process |
| `INBOX_READ_STATEMENT_TIMEOUT_MS` | 3000 | 1–60000 ms |
| `INBOX_READ_TIMEOUT_MS` | 5000 | 1000–120000 ms |

The request budget must exceed the SQL budget by at least one second. An
inconsistent configuration is increased to SQL timeout plus two seconds with a
warning. Queries do not begin when the remaining deadline cannot accommodate
the SQL timeout, network headroom and rollback. Changing settings requires a
process restart. Increasing either concurrency or SQL time raises database load.

The existing metrics listener exports `multica_inbox_read_active`,
`multica_inbox_read_queued`, `multica_inbox_read_rejected_total`,
`multica_inbox_read_duration_seconds`, `multica_inbox_read_query_duration_seconds`
and `multica_inbox_read_timeouts_total`.
Duration measures admitted transaction work including pool acquisition and
cleanup. Labels contain operation and rejection reason, never account IDs.
Correlate with primary CPU, connection acquisition latency, inbox failures and
daemon latency/5xx. A per-process budget limits work; it does not by itself
guarantee a database CPU ceiling or a daemon SLO.

## Rollout validation and rollback

Deploy server and clients independently. At three replicas the default admits
six concurrent inbox transactions; include surge replicas when sizing a rolling
deployment. Verify large-account latency and rejection rates before increasing
limits. Keep 503 responses visible in availability metrics.

Local regression coverage exercises database `pg_sleep` termination, pooled
setting restoration, bounded execution/queueing, canceled waiters, account
isolation, shared execution, all seven HTTP routes, and frontend cache/cancellation
behavior. Local checks do not substitute for the planned staging acceptance:
three API pods, normal daemon traffic, tenfold inbox refresh traffic and injected
2 s SQL delays. Confirm admission limits, primary CPU and daemon SLOs there.

Redis and read replicas are not dependencies of this guard. SQL optimization
and indexes remain a separate PR. Application rollback requires no database
rollback, but removing this guard also removes overload protection. Prefer
adjusting the optional settings after measurement while keeping protection on.
