-- name: CreateTaskMessage :one
-- The single-row writer. No production caller today — the daemon reports in
-- batches — but it is exported alongside CreateTaskMessages, and a writer that
-- persisted a message without advancing last_event_at would make the column
-- mean something different depending on which one ran. See CreateTaskMessages
-- for why the bump is a guarded, best-effort CTE rather than a second query.
-- created_at takes the column default here, and now() is transaction time, so
-- it is the same instant the inserted row gets.
WITH bumped AS (
    UPDATE agent_task_queue
    SET last_event_at = GREATEST(last_event_at, now())
    WHERE id = (
        SELECT id FROM agent_task_queue
        WHERE id = $2 AND (last_event_at IS NULL OR last_event_at < now())
        FOR NO KEY UPDATE SKIP LOCKED
    )
)
INSERT INTO task_message (id, task_id, seq, type, tool, content, input, output, output_truncated, call_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: CreateTaskMessages :many
-- Batch variant of CreateTaskMessage: persists a whole daemon-reported batch in
-- ONE statement — therefore one round trip and, more importantly, one commit
-- instead of one per message. Commit acknowledgement (IO:XactSync) is ~94% of
-- this SQL's load in production, so the commit count is the thing being
-- optimized; the round trip is a bonus.
--
-- The rows arrive as parallel arrays rather than as one jsonb document, even
-- though a jsonb document is the tidier Go side. content and output routinely
-- carry tens of KB and occasionally megabytes, and wrapping them in JSON makes
-- the server escape every byte a second time and Postgres parse the whole
-- envelope back out — measured at 1.2x the old per-row insert at 128KB and
-- ~1.8x at 1MB, i.e. a regression on exactly the most expensive requests, which
-- are also the ones least likely to be batched. Native text[] elements are
-- length-prefixed by the wire protocol, so they cost neither pass.
--
-- NULLIF is what makes per-row NULL expressible through a non-nullable []string
-- (the Go type sqlc gives a text[] parameter): it reproduces, exactly, the
-- `pgtype.Text{Valid: x != ""}` mapping the single-row query carries — empty
-- string means SQL NULL. input is passed as text and cast here for the same
-- reason; it is the one column that genuinely has to be parsed as JSON, because
-- it is a jsonb column. created_at also rides this transport so an older daemon
-- can omit its event time and keep the database-time fallback. output_truncated
-- uses the same trick because it is a genuinely tri-state boolean (NULL = the
-- reporting daemon did not measure it), and a bool[] parameter becomes a Go
-- []bool, which has no way to spell the third state.
--
-- Callers MUST still run the Postgres text sanitizer first. A NUL anywhere in
-- the batch fails the whole statement (GH #7098) — that is inherent to batching
-- into one statement, not to the parameter shape.
--
-- Atomicity is a deliberate side effect, not just a speedup: the per-message
-- loop this replaces could persist part of a batch and then fail, leaving the
-- transcript with a prefix of the batch and no way to complete it — the daemon
-- does not retry this endpoint. One statement makes the batch all-or-nothing,
-- which buys consistency; a batch that fails is still lost whole, so closing
-- the gap for real needs a retry plus a (task_id, seq) uniqueness rule.
--
-- The ORDER BY is a contract, not decoration. A bare `INSERT ... RETURNING`
-- has no defined row order, and the caller republishes these rows as realtime
-- events in the order they arrive — the per-row loop this replaces implicitly
-- published in request order, so the ordering has to be restored explicitly or
-- subscribers can see a batch out of order. seq is assigned by the daemon and
-- increases within a batch, so it is the request order.
--
-- The `bumped` CTE advances the owning task's last_event_at to the newest
-- created_at in this batch. It rides INSIDE the statement because a following
-- UPDATE would be a second round trip and a second commit on the endpoint whose
-- commit count was the thing being optimized, and it could fail on its own,
-- leaving messages persisted and the task row claiming the run went quiet
-- before them. A data-modifying CTE runs exactly once and to completion whether
-- or not the primary query reads its output, so `bumped` needs no reference
-- below to take effect.
--
-- The bump is BEST EFFORT, and deliberately so: persisting the transcript
-- outranks refreshing a liveness gauge. `SELECT ... FOR NO KEY UPDATE SKIP
-- LOCKED` is what buys that. Without it the batch takes a plain lock wait that
-- did not exist before this column — several statements in this codebase hold
-- FOR NO KEY UPDATE across a whole transaction on every task of a runtime
-- (runtime merge, offline-runtime failover, bulk cancel) — and with no
-- statement_timeout the batch parks a pool connection until that writer
-- commits. Measured on PostgreSQL 17 under a held FOR NO KEY UPDATE: the
-- unguarded form blocked to a 1.5s lock_timeout and returned an error, the
-- guarded form took 5ms. This endpoint has no daemon-side retry, so that error
-- loses the batch whole. Skipping the bump instead costs one tick of staleness,
-- which the next batch 500ms later repairs.
--
-- The `last_event_at < max(...)` predicate makes a batch that would not advance
-- the value a zero-row update rather than a dead tuple, and GREATEST keeps the
-- assignment itself monotonic so the invariant survives an edit to that
-- predicate. created_at is the reporting daemon's event clock when it supplies
-- one and database time when it does not, so two clocks reach this column;
-- within-batch commit order is not event order either, since the flusher's
-- posts can overlap. A value that walked backwards would report a live run as
-- newly silent.
--
-- The lock is taken on one row only, and the batch holds nothing else that
-- conflicts, so it can never be on both ends of a cycle — no deadlock. The
-- insert's own FK check cannot collide with it either: RI checks fire as
-- after-row triggers at end of statement, so `bumped` takes FOR NO KEY UPDATE
-- before the FK takes FOR KEY SHARE, and those two modes do not conflict in any
-- case.
WITH incoming AS (
    -- Several single-argument unnest calls in one SELECT list expand in
    -- lockstep (PostgreSQL 10+ set-returning-function semantics), which is the
    -- same row-wise zip the multi-argument unnest(a, b, ...) form gives — but
    -- sqlc's analyzer only knows the single-argument signature, so this is the
    -- shape that survives code generation.
    SELECT
        unnest(sqlc.arg('ids')::uuid[]) AS id,
        unnest(sqlc.arg('seqs')::int4[]) AS seq,
        unnest(sqlc.arg('types')::text[]) AS type,
        unnest(sqlc.arg('tools')::text[]) AS tool,
        unnest(sqlc.arg('call_ids')::text[]) AS call_id,
        unnest(sqlc.arg('contents')::text[]) AS content,
        unnest(sqlc.arg('inputs')::text[]) AS input,
        unnest(sqlc.arg('outputs')::text[]) AS output,
        unnest(sqlc.arg('created_ats')::text[]) AS created_at,
        unnest(sqlc.arg('output_truncations')::text[]) AS output_truncated
), inserted AS (
    INSERT INTO task_message (id, task_id, seq, type, tool, content, input, output, created_at, output_truncated, call_id)
    SELECT
        m.id,
        sqlc.arg('task_id')::uuid,
        m.seq,
        m.type,
        NULLIF(m.tool, ''),
        NULLIF(m.content, ''),
        NULLIF(m.input, '')::jsonb,
        NULLIF(m.output, ''),
        COALESCE(NULLIF(m.created_at, '')::timestamptz, now()),
        NULLIF(m.output_truncated, '')::bool,
        NULLIF(m.call_id, '')
    FROM incoming AS m
    RETURNING *
), bumped AS (
    UPDATE agent_task_queue
    SET last_event_at = GREATEST(last_event_at, (SELECT max(created_at) FROM inserted))
    WHERE id = (
        SELECT id FROM agent_task_queue
        WHERE id = sqlc.arg('task_id')::uuid
          AND (last_event_at IS NULL OR last_event_at < (SELECT max(created_at) FROM inserted))
        FOR NO KEY UPDATE SKIP LOCKED
    )
)
SELECT * FROM inserted ORDER BY seq ASC;

-- name: ListTaskMessages :many
SELECT * FROM task_message
WHERE task_id = $1
ORDER BY seq ASC;

-- name: ListTaskMessagesSince :many
SELECT * FROM task_message
WHERE task_id = $1 AND seq > $2
ORDER BY seq ASC;

-- name: DeleteTaskMessages :exec
DELETE FROM task_message
WHERE task_id = $1;
