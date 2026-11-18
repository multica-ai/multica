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

Summary requests are coalesced per authenticated account and generation. There
is no result cache. Requests arriving before a generation starts share its
result. Later arrivals share the next generation, which starts after the
current one finishes and has its own execution budget. A successful generation
returns immediately to its callers. A later refresh neither discards that
result nor accepts its old SQL snapshot as fresh data. There is at most one
running generation and one pending generation per account, both subject to the
shared admission budget. HTTP callers retain their own 5 s request deadline;
canceling one caller does not cancel its generation's work.
Summary callers, including canceled callers retained until completion, are
bounded to execution concurrency plus twenty per process.

Capacity exhaustion, connection acquisition deadlines and database statement
timeouts return `503` with `Retry-After: 1`. Other database errors remain `500`.
Web, desktop and mobile forward cancellation and skip automatic inbox retries
on 503. They retain cached data on a failed refresh; a subsequent inbox event
or explicit refresh can fetch again. Existing archive queries already disable
retries. Older clients may still retry, but
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

`TestInboxReadThreeInstancesBoundSlowSQL` provides a repeatable local stress
check on the checkout's isolated test database: three independent 25-connection
pools and guards, a burst of 30 inbox reads per instance (10x a three-read
refresh), 2 s SQL delays and 150 concurrent control reads. It checks execution
limits, overload rejection and capacity for unrelated database reads. Run with
the checkout's test `DATABASE_URL` configured:

```sh
cd server
go test -race ./internal/handler -run TestInboxReadThreeInstancesBoundSlowSQL -count=1 -v
```

This checks database admission locally; it does not exercise daemon HTTP
endpoints, measure staging CPU or validate a three-pod rollout.

Redis and read replicas are not dependencies of this guard. SQL optimization
and indexes remain a separate PR. Application rollback requires no database
rollback, but removing this guard also removes overload protection. Prefer
adjusting the optional settings after measurement while keeping protection on.
