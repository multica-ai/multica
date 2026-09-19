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

-- A claim may be prepared more than once before the daemon acknowledges it.
-- Keep that replaceable delivery set separate from the immutable receipt used
-- by replay coverage. Completion promotes only the final snapshot.
CREATE TABLE agent_task_comment_delivery_snapshot (
    task_id UUID NOT NULL REFERENCES agent_task_queue(id) ON DELETE CASCADE,
    comment_id UUID NOT NULL REFERENCES comment(id) ON DELETE CASCADE,
    comment_trigger_revision BIGINT NOT NULL CHECK (comment_trigger_revision > 0),
    claim_dispatched_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (task_id, comment_id)
);

CREATE INDEX idx_agent_task_comment_delivery_snapshot_comment
    ON agent_task_comment_delivery_snapshot (comment_id, comment_trigger_revision);

-- Preserve the last replaceable claim snapshot for in-flight tasks. The
-- current revision is safe only when it already existed at dispatch. A later
-- edit is deliberately left out: start/completion timestamps cannot prove
-- which revision was embedded in the earlier claim response.
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
  AND source.updated_at <= task.dispatched_at
ON CONFLICT (task_id, comment_id) DO NOTHING;

-- Only a successfully completed task has a final delivery set. Do not promote
-- dispatched/running/failed/cancelled rows: an unacknowledged claim may still
-- be replaced, while a failed attempt must remain eligible for recovery.
INSERT INTO comment_trigger_delivery_receipt (
    comment_id,
    comment_trigger_revision,
    agent_id,
    task_id,
    delivered_at
)
SELECT source.id,
       snapshot.comment_trigger_revision,
       task.agent_id,
       task.id,
       task.completed_at
FROM agent_task_queue AS task
JOIN agent_task_comment_delivery_snapshot AS snapshot ON snapshot.task_id = task.id
JOIN comment AS source ON source.id = snapshot.comment_id
WHERE task.status = 'completed'
  AND task.completed_at IS NOT NULL
ON CONFLICT (comment_id, comment_trigger_revision, agent_id) DO NOTHING;
