-- Revert FIR-2512: remove 'project' subject_type and capability index.
DROP INDEX IF EXISTS idx_cerebro_workspace_grant_capability;

-- Remove project grants before tightening the constraint.
DELETE FROM cerebro_workspace_grant WHERE subject_type = 'project';

ALTER TABLE cerebro_workspace_grant
    DROP CONSTRAINT IF EXISTS cerebro_workspace_grant_subject_type_check;

ALTER TABLE cerebro_workspace_grant
    ADD CONSTRAINT cerebro_workspace_grant_subject_type_check CHECK (
        subject_type IN ('member', 'agent', 'group', 'role', 'workspace_default')
    );
