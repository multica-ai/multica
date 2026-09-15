# Operator-driven maintenance jobs

Maintenance runs through a separate HTTP listener in the API container. There
is no startup backfill, scheduler, background executor, or automatic restart.
The initial registered processor is `issue_status_category`, version 1. The
shared `maintenance_job` table also supports later database repair/backfill
processors with their own parameter, checkpoint and result schemas.

## Enable the local entrypoint

Set `MAINTENANCE_PORT` to an unused port (for example 6061) when deploying the
API. Unset means disabled. The bind address is always **127.0.0.1** and cannot
be overridden. Invalid configuration or a bind failure disables only this
listener and logs an error; check the listener before attempting maintenance.

Do not publish this port, add it to a Service/Ingress, or proxy these routes.
There is deliberately no application authentication. Access relies on container
exec/operational permissions and network-namespace isolation; another process
in the same namespace can also connect. Host-networked containers share the
host loopback namespace. This API is never mounted on the public router.
Avoid placing credentials or user content in job parameters or logs.

The backend image includes `/app/maintenance`, a standalone Go HTTP client.
It needs no Python, curl, source checkout, or database credentials. It only
connects to loopback, ignores HTTP proxy variables, and refuses redirects.
Merely enabling the listener does not create or advance a job.
No production execution is part of submitting this PR.

Enable the listener using the deployment's existing configuration/release flow:

- Helm: set `backend.config.maintenancePort: "6061"` in the release values.
  The ConfigMap supplies `MAINTENANCE_PORT`; its checksum rolls the backend.
  Neither the Service nor the container's published ports gains a maintenance port.
- Compose: set `MAINTENANCE_PORT=6061` in the deployment `.env`, then recreate
  the backend with `docker compose -f docker-compose.selfhost.yml up -d backend`.
  The maintenance port is not published to the host.

Run every command below in the **target backend container**. For example:

```bash
# Compose; omit -T only when an interactive terminal is desired.
docker compose -f docker-compose.selfhost.yml exec -T backend /app/maintenance status --job JOB_UUID
# Kubernetes; select a backend pod from the intended release/namespace.
kubectl exec -n NAMESPACE POD -c backend -- /app/maintenance status --job JOB_UUID
```

Both forms inherit `MAINTENANCE_PORT` from the container. `--port 6061` can
explicitly select another configured loopback port. Use `create --help` or
`run --help` to check that the deployed image includes the command. A successful
`status` or `create` request confirms the listener is reachable; a connection
error means configuration/listener startup must be checked before continuing.
No public API token or kubectl port-forward is required. Container exec is a
privileged operational capability. Keep the attached exec session open until
the command returns; this is not a background service.

## Category backfill prerequisites

1. Deploy PR1's matching migration runner through 478. Inspect the actual CHECKs
   and functions, not just the migration ledger. Never execute historical 469
   directly. The API checks the ledger, compatibility vocabulary, mapping
   function, terminal spellings and its maintenance indexes.
2. Finish the full compatible backend rollout, including every writer. Capture
   deployment revision/replica evidence: the database cannot prove this.
   `parameters.writers_upgraded=true` is the operator's explicit attestation,
   not automatic proof of deployment.
3. Apply migrations 485–488. They only create the maintenance table and three
   unique indexes; indexes run concurrently in separate migration files with
   invalid-index cleanup hooks. No issue-status rows are changed.
4. Record baseline API latency/error rate, row-lock waits, DB I/O, WAL rate and
   replica lag. Choose batch/delay limits against the environment's existing
   alert budgets, beginning with a bounded canary. Pause if any metric exceeds
   its agreed budget; reduce batch size/increase delay before resuming.
   No fixed throughput or historical row count is a current safety guarantee.

## Dry-run, then apply

The driver requires explicit batch size, delay and both SQL timeouts for
**create and configure**, including dry-runs. The following values are an
example for a monitored canary, not production defaults or a throughput promise.
Select them against the target database's latency/lock/replica-lag budgets.
Commands are shown without the container-exec prefix for readability:

```bash
/app/maintenance create --idempotency-key status-category-v1-check-001 \
  --batch-size 500 --delay-ms 100 --lock-timeout-ms 500 --statement-timeout-ms 3000
```

Keep the returned `id`. Creation defaults to dry-run; dry-run persists its
progress/report but never changes category data. One command can drive the
whole survey, with each HTTP request still advancing only one short transaction:

```bash
# Canary: exits 2 (incomplete) unless this one batch completes the job.
/app/maintenance run --job JOB_UUID --max-batches 1 --max-seconds 30
# After checking canary metrics, run a bounded window automatically.
/app/maintenance run --job JOB_UUID --max-batches 3000 --max-seconds 1800
/app/maintenance status --job JOB_UUID
```

For 600,000 rows at batch size 500, a dry-run needs about 1,201 advances;
apply plus verification needs about 2,402, including end-of-pass detection.
The 3,000-batch/1,800-second example has headroom for that row count but is
still a hard stop. At a 100ms batch delay, waiting alone is about 240 seconds
for apply plus verification, before database and HTTP time. A smaller batch,
more rows, contention, or retries can exceed either budget. Re-run `status`,
inspect metrics, and explicitly start another bounded `run` if incomplete.
No one needs to invoke thousands of curl commands manually.

`run` writes the observed job JSON after each advance and exits:

- **0:** the persisted job is `completed` (for dry-run, only the survey completed).
- **2:** incomplete because either batch or elapsed-time budget ran out.
- **1:** invalid arguments, cancellation, a paused/cancelled job, or an API error.

Time includes requests, delay and retries; sleeps and requests are cancellable.
A timeout or transport error can leave a final commit's acknowledgement unknown.
Read `status` before continuing; do not interpret incomplete/error as rollback.
Keep stdout and stderr in the operator session log. Process exit never restarts
itself or schedules later work.

Once dry-run is completed, create a **new** apply record with a new key:

```bash
/app/maintenance create --idempotency-key status-category-v1-apply-001 \
  --apply --parameters '{"writers_upgraded":true}' \
  --batch-size 500 --delay-ms 100 --lock-timeout-ms 500 --statement-timeout-ms 3000
```

Use the batch/delay/timeouts selected from the dry-run and monitored apply
canary. Run one batch first, check the metrics, then continue with an explicit
budget as above. Dry-run cannot measure update WAL/lock impact; the apply
canary is required too. Tuning a paused record preserves the cursor.

Creating with the same idempotency key and identical normalized parameters
returns the original job. Reusing that key with different input conflicts.
Only one ready/paused record per job type and scope is permitted, including
dry-runs. Finish or explicitly cancel the existing record before starting
another. Different logical scopes must be disjoint; new processors that allow
overlapping scopes must implement an additional conflict guard.

## Pause, tune, resume, failures

Stopping calls stops progress. Query `status`, then use its revision for an
intentional mutation. These operations may return busy while a short batch
holds the job row; query/retry after it finishes. A stale revision performs no
work. Each successful mutation returns the new revision:

```bash
/app/maintenance pause --job JOB_UUID --revision CURRENT_REVISION
/app/maintenance configure --job JOB_UUID --revision PAUSED_REVISION \
  --batch-size 100 --delay-ms 1000 --lock-timeout-ms 250 --statement-timeout-ms 2000
/app/maintenance resume --job JOB_UUID --revision CONFIGURED_REVISION
/app/maintenance run --job JOB_UUID --max-batches 3000 --max-seconds 1800
# To stop permanently, using the latest revision:
/app/maintenance cancel --job JOB_UUID --revision CURRENT_REVISION
```

Cancellation preserves already committed data and the global audit record;
it does not reverse a backfill. Configuration replaces operational limits,
preserving checkpoint and immutable parameters. Limits: batch 1–5000,
delay 1–60000ms, and 1 <= lock timeout <= statement timeout <= 5000ms.
Increasing/decreasing limits cannot erase an existing delay. The raw HTTP API
retains default normalization for omitted/zero limits; operational runs should
use the packaged driver, which requires explicit values.

The underlying routes remain `POST /maintenance/jobs`,
`GET /maintenance/jobs/{id}`, and `POST /maintenance/jobs/{id}/{action}`.
Mutations carry `{"revision":N}`; configure also carries `options`.
The driver retries an ambiguous advance using its **original** revision.
409 with a changed revision acknowledges current state without advancing again;
429 respects the database delay; 409 without a job retries busy with backoff.

Each batch locks the maintenance row NOWAIT, scans an indexed ID page, updates
only still-legacy categories from their current values, and commits data,
checkpoint, counters and revision in one transaction. It includes system,
custom and archived rows, preserving all fields except category. No issue
update events or agent runs are emitted. It does not use SKIP LOCKED or OFFSET.

On SQL failure the batch savepoint rolls back all data changes. The outer
transaction records a paused state and error without advancing the checkpoint.
Timeout/deadlock/serialization errors are marked retryable; the driver retries
with capped exponential backoff (default 3, configurable up to 10), using the
new revision to resume that failed batch. Other errors stop the driver.
A previously paused job is not automatically resumed without `--resume`.
There is no automatic retry after process exit. The driver has batch and wall
time budgets; Ctrl-C stops driving requests.

If the connection/container disappears before commit, data and progress both
roll back. If it disappears after commit, another replica can resume from the
stored cursor. If cancellation prevents even the error record from committing,
the checkpoint stays unchanged and server logs/client transport error provide
the failure evidence; inspect GET before retrying. Graceful shutdown drains
the internal listener before closing the database pool.

## Completion and evidence

Application runs move from the scan phase into a fresh, independently paged
verification phase. Verification rejects residual legacy values, unknown
categories and noncanonical system pairs. A bad page pauses with the first
offending status ID. Fix the cause, then resume; for residuals behind the scan
cursor, cancel and start a new pass after eliminating old writers.

Only a complete verification pass produces `status=completed` and
`result.remaining_legacy=0`. That report depends on the no-old-writers rollout
precondition; a paged scan cannot certify that an old writer will never write
again. Dry-run completion only means the read-only survey completed, not that
the data has been converted. Dry-run counts and `would_update` are observations
during a paged scan, not a globally consistent snapshot. Progress is cumulative;
`updated` counts actual committed updates, while `would_update` counts old
values observed before any concurrent application normalization. Neither is a
live remaining-row count. Use a fresh dry-run to refresh backlog evidence.

Save the job JSON, deployment evidence, monitoring/canary results and operator
session log. Completed/cancelled rows are retained; repeated maintenance creates
new records rather than resetting history. Down migrations intentionally retain
state and indexes. Keep the compatible backend and fix forward on failures.

PR3 remains separate: no strict CHECK, validation DDL, legacy-reader removal,
or wire-protocol retirement is triggered by this job. Installed-client API
compatibility remains after backfill completion.

## Adding a processor

Register a code-defined `maintenance.Processor` with a type and version. Validate
scope and parameters, implement preflight and one bounded Step, and return
versioned checkpoint/progress/result JSON objects plus a completion flag.
A processor uses only the supplied primary-database transaction. Unknown
types/versions and malformed state fail closed. This is not an arbitrary SQL
execution endpoint. Checkpoint atomicity does not cover external side effects;
such processors need their own idempotency/recovery design before registration.
