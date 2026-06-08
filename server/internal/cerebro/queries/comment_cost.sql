-- CEREBRO-PATCH(cerebro-comment-task): FIR-39 — per-comment cost on issues +
-- channels. Mirrors chat_message_cost.sql (FIR-31) but keys on the comment <->
-- task link table because the upstream `comment` row has no task_id column.

-- name: LinkCommentToTask :exec
-- Records that an agent comment was authored by the given task so the per-comment
-- cost badge can find it later. Idempotent on comment_id (one comment = one
-- authoring task): an agent retrying a duplicate write would otherwise crash
-- the CreateComment transaction on the PK. Called from the comment handler in
-- a CEREBRO-PATCH after the upstream insert; agent comments only.
INSERT INTO cerebro_comment_task (comment_id, task_id, issue_id, workspace_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (comment_id) DO NOTHING;

-- name: GetIssueCommentCosts :many
-- Per-task token totals + cost for every task that authored at least one
-- comment on this issue, paired with the **last** comment that task produced.
-- The badge attaches to that final comment so a run that posts progress +
-- result does not double-count (FIR-39 design rule). Split by model so pricing
-- stays correct when a single run spans models; the handler then sums per task
-- and prefers the gateway-reported spend over the pricing estimate — identical
-- to the rule in GetChatSessionMessageCosts so the per-comment numbers reconcile
-- with the existing per-issue total (JEH-736 / IssueUsageSummary).
SELECT
    last_comment.task_id                            AS task_id,
    last_comment.comment_id                         AS comment_id,
    tu.model                                        AS model,
    COALESCE(SUM(tu.input_tokens), 0)::bigint       AS total_input_tokens,
    COALESCE(SUM(tu.output_tokens), 0)::bigint      AS total_output_tokens,
    COALESCE(SUM(tu.cache_read_tokens), 0)::bigint  AS total_cache_read_tokens,
    COALESCE(SUM(tu.cache_write_tokens), 0)::bigint AS total_cache_write_tokens,
    COALESCE(SUM(tu.cost_cents), 0)::bigint         AS total_cost_cents
FROM (
    SELECT DISTINCT ON (cct.task_id)
        cct.task_id,
        cct.comment_id
    FROM cerebro_comment_task cct
    WHERE cct.issue_id = $1
    ORDER BY cct.task_id, cct.created_at DESC, cct.comment_id DESC
) AS last_comment
JOIN task_usage tu ON tu.task_id = last_comment.task_id
GROUP BY last_comment.task_id, last_comment.comment_id, tu.model;
