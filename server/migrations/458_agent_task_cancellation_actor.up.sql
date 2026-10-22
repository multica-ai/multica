-- Additive cancellation provenance for run history. Nullable columns keep
-- historical rows and mixed-version deployments compatible: old cancellations
-- continue to render the existing plain status.
ALTER TABLE agent_task_queue
    ADD COLUMN cancelled_by_type TEXT,
    ADD COLUMN cancelled_by_id UUID,
    ADD COLUMN cancelled_by_name TEXT;
