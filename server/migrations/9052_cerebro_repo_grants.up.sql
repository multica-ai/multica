-- Extend cerebro_workspace_grant subject_type to include 'project' (FIR-2512).
--
-- 'project' subject_type allows grants scoped to a Multica project:
-- "agents running tasks in project P may perform capability C on resource R".
-- This adds project as a chain layer between workspace_default and group.
--
-- Also introduces a partial index for fast daemon repo-capability lookups.

ALTER TABLE cerebro_workspace_grant
    DROP CONSTRAINT IF EXISTS cerebro_workspace_grant_subject_type_check;

ALTER TABLE cerebro_workspace_grant
    ADD CONSTRAINT cerebro_workspace_grant_subject_type_check CHECK (
        subject_type IN ('member', 'agent', 'group', 'role', 'workspace_default', 'project')
    );

-- Index for repo capability checks: (workspace, capability prefix, status)
-- speeds up the ListCerebroWorkspaceGrants fetch the daemon endpoint issues.
CREATE INDEX IF NOT EXISTS idx_cerebro_workspace_grant_capability
    ON cerebro_workspace_grant (workspace_id, capability, status)
    WHERE status = 'active';
