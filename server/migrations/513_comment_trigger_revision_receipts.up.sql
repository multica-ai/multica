ALTER TABLE comment
    ADD COLUMN trigger_revision BIGINT NOT NULL DEFAULT 1
        CHECK (trigger_revision > 0);

-- Establish a monotonic baseline for existing rows. Future presentation-only
-- mutations (reactions/resolution) keep this value stable; only an edited
-- execution instruction advances it.
UPDATE comment
SET trigger_revision = revision;

ALTER TABLE comment_trigger_outbox
    ADD COLUMN comment_trigger_revision BIGINT NOT NULL DEFAULT 1
        CHECK (comment_trigger_revision > 0);

UPDATE comment_trigger_outbox AS outbox
SET comment_trigger_revision = source.trigger_revision
FROM comment AS source
WHERE source.id = outbox.comment_id;

CREATE TABLE comment_trigger_delivery_receipt (
    comment_id UUID NOT NULL REFERENCES comment(id) ON DELETE CASCADE,
    comment_trigger_revision BIGINT NOT NULL CHECK (comment_trigger_revision > 0),
    agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    task_id UUID NOT NULL,
    delivered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (comment_id, comment_trigger_revision, agent_id)
);

CREATE INDEX idx_comment_trigger_delivery_receipt_task
    ON comment_trigger_delivery_receipt (task_id);

-- Preserve idempotency for comments that were already embedded before this
-- migration. A current revision is safe to backfill only when it had reached
-- that revision no later than the task dispatch. If the comment was edited
-- after dispatch, leave it uncovered so the new revision is delivered.
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
