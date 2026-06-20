-- FIR-1657: let a project's sprints accept issues from other projects.
--
-- The flag lives on the sprint-owning project's settings row: when TRUE, an
-- issue from any other project in the same workspace may be assigned to this
-- project's sprints. Default FALSE keeps every project closed until its owner
-- opts in. The picker and the assign endpoint both read this flag, so it is
-- the single source of truth for cross-project sprint selection.

ALTER TABLE cerebro_sprint_settings
    ADD COLUMN IF NOT EXISTS accepts_external_issues BOOLEAN NOT NULL DEFAULT FALSE;
