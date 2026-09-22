CREATE UNIQUE INDEX CONCURRENTLY workflow_work_item_active_idx ON workflow_work_item (run_id, node_id, kind) WHERE status = 'open';
