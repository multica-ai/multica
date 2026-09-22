CREATE UNIQUE INDEX CONCURRENTLY workflow_work_item_active_v2_idx ON workflow_work_item (activation_id, kind) WHERE status = 'open' AND activation_id IS NOT NULL;
