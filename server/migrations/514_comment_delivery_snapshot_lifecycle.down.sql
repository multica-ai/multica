-- Restore the claim-stage approximation used by the pre-514 application.
-- This is intentionally conservative: only a comment that had its current
-- revision by dispatch time is treated as delivered.
DELETE FROM comment_trigger_delivery_receipt;

INSERT INTO comment_trigger_delivery_receipt (
    comment_id,
    comment_trigger_revision,
    agent_id,
    task_id,
    delivered_at
)
SELECT source.id,
       source.trigger_revision,
       task.agent_id,
       task.id,
       task.dispatched_at
FROM agent_task_queue AS task
CROSS JOIN LATERAL unnest(task.delivered_comment_ids) AS delivered(comment_id)
JOIN comment AS source ON source.id = delivered.comment_id
WHERE task.dispatched_at IS NOT NULL
  AND source.updated_at <= task.dispatched_at
ON CONFLICT (comment_id, comment_trigger_revision, agent_id) DO NOTHING;

DROP TABLE IF EXISTS agent_task_comment_delivery_snapshot;
