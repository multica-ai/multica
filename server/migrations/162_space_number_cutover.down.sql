-- Reverse of the space-numbering cutover (Migration B).
--
-- Restores the additive 161 state: space_id nullable again, space-scoped
-- uniqueness dropped, legacy workspace-scoped uniqueness restored.
--
-- WARNING: re-adding uq_issue_workspace_number WILL FAIL if per-space numbering
-- has already minted overlapping numbers across spaces in the same workspace
-- (e.g. ENG-1 and DES-1 both exist as issue.number = 1 under one workspace).
-- That failure is inherent: once spaces number independently from 1, the
-- workspace-number space is no longer unique. This down migration deliberately
-- attempts the constraint and lets Postgres error out rather than silently
-- dropping the constraint or renumbering data. Recovery requires renumbering
-- issues into a single monotonic per-workspace sequence before this migration
-- can complete — or accepting that the cutover is effectively one-way once
-- multi-space numbers have been minted.
--
-- The re-backfill of NULL space_id done by the up migration is not reversed:
-- which rows were originally NULL is not recoverable, and 161's own down
-- migration drops the space_id column entirely anyway.

ALTER TABLE issue DROP CONSTRAINT IF EXISTS uq_issue_space_number;

ALTER TABLE issue DROP CONSTRAINT IF EXISTS fk_issue_workspace_project_space;
ALTER TABLE issue
    ADD CONSTRAINT issue_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES project(id) ON DELETE SET NULL;

ALTER TABLE autopilot DROP CONSTRAINT IF EXISTS fk_autopilot_workspace_project_space;
ALTER TABLE autopilot
    ADD CONSTRAINT autopilot_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES project(id) ON DELETE SET NULL;

ALTER TABLE issue DROP CONSTRAINT IF EXISTS fk_issue_workspace_parent_space;
ALTER TABLE issue
    ADD CONSTRAINT issue_parent_issue_id_fkey
    FOREIGN KEY (parent_issue_id) REFERENCES issue(id) ON DELETE SET NULL;
ALTER TABLE issue DROP CONSTRAINT IF EXISTS uq_issue_workspace_id_space_id;
ALTER TABLE project DROP CONSTRAINT IF EXISTS uq_project_workspace_id_space_id;

ALTER TABLE autopilot ALTER COLUMN space_id DROP NOT NULL;
ALTER TABLE issue ALTER COLUMN space_id DROP NOT NULL;
ALTER TABLE project ALTER COLUMN space_id DROP NOT NULL;

-- May fail by design; see the WARNING above.
ALTER TABLE issue ADD CONSTRAINT uq_issue_workspace_number UNIQUE (workspace_id, number);
