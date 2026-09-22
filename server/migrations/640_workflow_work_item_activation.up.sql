CREATE INDEX CONCURRENTLY workflow_work_item_activation_idx ON workflow_work_item (workspace_id, activation_id, status, due_at, id);
