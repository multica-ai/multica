-- The job's scan: rows not yet done, ordered by when they are next due.
CREATE INDEX CONCURRENTLY IF NOT EXISTS knowledge_tenant_deprovision_due_idx
    ON knowledge_tenant_deprovision (next_attempt_at)
    WHERE state <> 'done';
