-- name: CreateTaskMessage :one
INSERT INTO task_message (id, task_id, seq, type, tool, content, input, output)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
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
-- it is a jsonb column.
--
-- Callers MUST still run the Postgres text sanitizer first. A NUL anywhere in
-- the batch fails the whole statement (GH #7098) — that is inherent to batching
-- into one statement, not to the parameter shape.
--
-- Atomicity is a deliberate side effect, not just a speedup: the per-message
-- loop this replaces could persist part of a batch and then fail. The daemon
-- now retries the exact batch, while (task_id, seq) uniqueness and DO NOTHING
-- make a lost-response replay idempotent and keep realtime events single-shot.
--
-- The ORDER BY is a contract, not decoration. A bare `INSERT ... RETURNING`
-- has no defined row order, and the caller republishes these rows as realtime
-- events in the order they arrive — the per-row loop this replaces implicitly
-- published in request order, so the ordering has to be restored explicitly or
-- subscribers can see a batch out of order. seq is assigned by the daemon and
-- increases within a batch, so it is the request order.
WITH raw_incoming AS (
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
        unnest(sqlc.arg('contents')::text[]) AS content,
        unnest(sqlc.arg('inputs')::text[]) AS input,
        unnest(sqlc.arg('outputs')::text[]) AS output
), incoming AS (
    SELECT
        id,
        seq,
        type,
        NULLIF(tool, '') AS tool,
        NULLIF(content, '') AS content,
        NULLIF(input, '')::jsonb AS input,
        NULLIF(output, '') AS output
    FROM raw_incoming
), inserted AS (
    INSERT INTO task_message (id, task_id, seq, type, tool, content, input, output)
    SELECT
        m.id,
        sqlc.arg('task_id')::uuid,
        m.seq,
        m.type,
        m.tool,
        m.content,
        m.input,
        m.output
    FROM incoming AS m
    ON CONFLICT (task_id, seq) DO NOTHING
    RETURNING *
), resolved AS (
    SELECT inserted.*, true AS was_inserted, true AS payload_matches
    FROM inserted

    UNION ALL

    SELECT
        existing.*,
        false AS was_inserted,
        existing.type = incoming.type
            AND existing.tool IS NOT DISTINCT FROM incoming.tool
            AND existing.content IS NOT DISTINCT FROM incoming.content
            AND existing.input IS NOT DISTINCT FROM incoming.input
            AND existing.output IS NOT DISTINCT FROM incoming.output
            AS payload_matches
    FROM incoming
    JOIN task_message AS existing
      ON existing.task_id = sqlc.arg('task_id')::uuid
     AND existing.seq = incoming.seq
)
SELECT * FROM resolved ORDER BY seq ASC;

-- name: LockTaskForTranscriptWrite :one
SELECT status FROM agent_task_queue
WHERE id = $1
FOR UPDATE;

-- name: ListTaskMessages :many
SELECT * FROM task_message
WHERE task_id = $1
ORDER BY seq ASC;

-- name: ListTaskMessagesSince :many
SELECT * FROM task_message
WHERE task_id = $1 AND seq > $2
ORDER BY seq ASC;

-- name: GetTaskMessageSeqSummary :one
SELECT
    COUNT(*)::int AS message_count,
    COALESCE(MIN(seq), 0)::int AS min_seq,
    COALESCE(MAX(seq), 0)::int AS max_seq
FROM task_message
WHERE task_id = $1;

-- name: DeleteTaskMessages :exec
DELETE FROM task_message
WHERE task_id = $1;
