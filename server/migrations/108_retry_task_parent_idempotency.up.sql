-- Ensure retry creation is idempotent at the database boundary: each failed
-- parent task can have only one direct retry child. If historical duplicate
-- children exist, keep the oldest back-pointer and detach the later duplicates
-- rather than deleting task history.

WITH ranked_retry_children AS (
  SELECT
    id,
    row_number() OVER (
      PARTITION BY parent_task_id
      ORDER BY created_at ASC, id ASC
    ) AS retry_rank
  FROM agent_task_queue
  WHERE parent_task_id IS NOT NULL
)
UPDATE agent_task_queue
SET parent_task_id = NULL
WHERE id IN (
  SELECT id
  FROM ranked_retry_children
  WHERE retry_rank > 1
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_task_queue_retry_parent_unique
  ON agent_task_queue(parent_task_id)
  WHERE parent_task_id IS NOT NULL;
