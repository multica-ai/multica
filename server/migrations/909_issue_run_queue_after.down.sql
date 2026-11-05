ALTER TABLE issue DROP COLUMN IF EXISTS run_after_issue_ids;
ALTER TABLE issue DROP COLUMN IF EXISTS run_queued;
ALTER TABLE issue ADD COLUMN queue_order INTEGER CHECK (queue_order IS NULL OR queue_order >= 1);
