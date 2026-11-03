-- One deprovision per workspace. A deleted workspace id is never reused, so the
-- row for it is written once and then only its state changes.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS knowledge_tenant_deprovision_workspace_uidx
    ON knowledge_tenant_deprovision (workspace_id);
