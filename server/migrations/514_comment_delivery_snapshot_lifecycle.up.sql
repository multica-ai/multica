-- Compatibility repair for databases that applied migration 513 before claim
-- snapshots were separated from immutable completion receipts.
CREATE TABLE IF NOT EXISTS agent_task_comment_delivery_snapshot (
    task_id UUID NOT NULL REFERENCES agent_task_queue(id) ON DELETE CASCADE,
    comment_id UUID NOT NULL REFERENCES comment(id) ON DELETE CASCADE,
    comment_trigger_revision BIGINT NOT NULL CHECK (comment_trigger_revision > 0),
    claim_dispatched_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (task_id, comment_id)
);

CREATE INDEX IF NOT EXISTS idx_agent_task_comment_delivery_snapshot_comment
    ON agent_task_comment_delivery_snapshot (comment_id, comment_trigger_revision);

INSERT INTO agent_task_comment_delivery_snapshot (
    task_id,
    comment_id,
    comment_trigger_revision,
    claim_dispatched_at
)
SELECT task.id,
       source.id,
       source.trigger_revision,
       task.dispatched_at
FROM agent_task_queue AS task
CROSS JOIN LATERAL unnest(task.delivered_comment_ids) AS delivered(comment_id)
JOIN comment AS source ON source.id = delivered.comment_id
WHERE task.dispatched_at IS NOT NULL
  -- Be conservative: a later start/completion cannot prove that a revision
  -- edited after dispatch was present in the earlier claim response.
  AND source.updated_at <= task.dispatched_at
ON CONFLICT (task_id, comment_id) DO UPDATE
SET comment_trigger_revision = EXCLUDED.comment_trigger_revision,
    claim_dispatched_at = EXCLUDED.claim_dispatched_at;

-- Receipts written by the old claim-stage contract are not trustworthy after
-- a reclaim replaces delivered_comment_ids. Rebuild only from completed tasks'
-- final snapshots.
DELETE FROM comment_trigger_delivery_receipt;

INSERT INTO comment_trigger_delivery_receipt (
    comment_id,
    comment_trigger_revision,
    agent_id,
    task_id,
    delivered_at
)
SELECT snapshot.comment_id,
       snapshot.comment_trigger_revision,
       task.agent_id,
       task.id,
       task.completed_at
FROM agent_task_queue AS task
JOIN agent_task_comment_delivery_snapshot AS snapshot ON snapshot.task_id = task.id
WHERE task.status = 'completed'
  AND task.completed_at IS NOT NULL
ON CONFLICT (comment_id, comment_trigger_revision, agent_id) DO NOTHING;
