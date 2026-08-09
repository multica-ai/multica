DROP TABLE IF EXISTS cerebro_workflow_hook_test_evidence;

DELETE FROM cerebro_workflow_hook_run WHERE draft_revision_id IS NOT NULL;

ALTER TABLE cerebro_workflow_hook_run
    DROP CONSTRAINT IF EXISTS hook_run_policy_or_draft_check,
    DROP CONSTRAINT IF EXISTS hook_run_draft_revision_fk,
    DROP COLUMN IF EXISTS draft_revision_id,
    ALTER COLUMN policy_id SET NOT NULL;

ALTER TABLE cerebro_workflow_hook_draft_revision
    DROP CONSTRAINT IF EXISTS hook_draft_revision_workspace_id_unique;

DROP TABLE IF EXISTS cerebro_workflow_hook_event_journal;
