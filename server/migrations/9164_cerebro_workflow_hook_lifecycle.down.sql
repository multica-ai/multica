ALTER TABLE cerebro_workflow_hook_family
    DROP CONSTRAINT IF EXISTS hook_family_current_draft_fk,
    DROP CONSTRAINT IF EXISTS hook_family_active_policy_fk;

DROP TABLE IF EXISTS cerebro_workflow_hook_draft_revision;
DROP TABLE IF EXISTS cerebro_workflow_hook_draft_series;
DROP TABLE IF EXISTS cerebro_workflow_hook_family;

ALTER TABLE cerebro_workflow_hook_policy
    DROP CONSTRAINT IF EXISTS hook_policy_family_workspace_unique;
