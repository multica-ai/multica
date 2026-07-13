-- Backfill source_task_id on agent comments posted DURING a run's
-- execution (between started_at and completed_at) that were not
-- stamped by the CLI path (X-Task-ID header absent or the stamping
-- code path not reached).
--
-- Migrations 158 and 159 left a gap: 158 covered reply-comments
-- whose parent_id → trigger_comment_id path resolves to a run; 159
-- covered completion-fallback comments whose created_at > completed_at.
-- Neither covers comments created during execution (started_at ≤
-- created_at < completed_at) on the same (issue, agent) — the
-- CLI-path stamping at handler/comment.go:1257-1264 requires the
-- X-Task-ID header, and when it is absent the comment's source_task_id
-- stays NULL even though the comment clearly belongs to that run.
--
-- Strategy: for each un-stamped agent comment, find the run on the
-- same (issue, agent) whose execution window contains the comment's
-- created_at. When multiple runs qualify (rare but possible when two
-- runs on the same issue+agent overlap in time due to retry), pick
-- the one whose started_at is most recent — it is the run that was
-- actually in progress when the comment was posted.
--
-- Safety properties:
-- 1. WHERE c.source_task_id IS NULL — idempotent, safe to re-run.
-- 2. The JOIN is on (issue_id, agent_id, time window) — workspace-
--    bounded by construction (a run on an issue can only produce
--    comments on the same issue).
-- 3. DISTINCT ON (c.id) with ORDER BY t.started_at DESC picks the
--    run that was most recently started (the one in progress).
-- 4. The time-window join is precise: started_at ≤ created_at < completed_at.
--    Comments after completed_at belong to the completion path (migration
--    159); comments before started_at were not produced by this run.
--
-- Disambiguation: on the production dataset, the same (issue, agent)
-- pair rarely has overlapping execution windows. When it does, the
-- "most recently started" heuristic is correct — the later run is the
-- one that was active when the comment was posted.
WITH matches AS (
    SELECT DISTINCT ON (c.id)
        c.id   AS comment_id,
        t.id   AS task_id
    FROM comment c
    JOIN agent_task_queue t
        ON  t.issue_id    = c.issue_id
        AND t.agent_id     = c.author_id
        AND t.started_at   <= c.created_at
        AND c.created_at   < t.completed_at
        AND t.completed_at IS NOT NULL
    WHERE c.author_type    = 'agent'
      AND c.source_task_id IS NULL
      AND c.type            = 'comment'
    ORDER BY c.id, t.started_at DESC
)
UPDATE comment c
SET    source_task_id = m.task_id
FROM   matches m
WHERE  c.id = m.comment_id;