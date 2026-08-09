-- CEREBRO-PATCH(comment-gate-blocker-line): FIR-4727 — when a before.message.send
-- gate rejects an agent comment, drop one `blocker` line into that agent's run
-- timeline (task_message) so a human watching the run modal sees the block
-- instead of only an opaque 422. Reuses the existing task_message table the run
-- modal already renders; no new table, no new UI.

-- name: NextTaskMessageSeq :one
-- Next sequence number after the agent's own trace events, so the blocker line
-- sorts to the end of the run timeline.
SELECT (COALESCE(MAX(seq), 0) + 1)::int AS seq
FROM task_message
WHERE task_id = $1;

-- name: InsertBlockerTaskMessage :exec
INSERT INTO task_message (task_id, seq, type, content)
VALUES ($1, $2, 'blocker', $3);
