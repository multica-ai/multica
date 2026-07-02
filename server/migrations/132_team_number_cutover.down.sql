-- Reverse of the team-numbering cutover (Migration B).
--
-- Restores the additive 131 state: team_id nullable again, team-scoped
-- uniqueness dropped, legacy workspace-scoped uniqueness restored.
--
-- WARNING: re-adding uq_issue_workspace_number WILL FAIL if per-team numbering
-- has already minted overlapping numbers across teams in the same workspace
-- (e.g. ENG-1 and DES-1 both exist as issue.number = 1 under one workspace).
-- That failure is inherent: once teams number independently from 1, the
-- workspace-number space is no longer unique. This down migration deliberately
-- attempts the constraint and lets Postgres error out rather than silently
-- dropping the constraint or renumbering data. Recovery requires renumbering
-- issues into a single monotonic per-workspace sequence before this migration
-- can complete — or accepting that the cutover is effectively one-way once
-- multi-team numbers have been minted.
--
-- The re-backfill of NULL team_id done by the up migration is not reversed:
-- which rows were originally NULL is not recoverable, and 131's own down
-- migration drops the team_id column entirely anyway.

ALTER TABLE issue DROP CONSTRAINT IF EXISTS uq_issue_team_number;

ALTER TABLE autopilot ALTER COLUMN team_id DROP NOT NULL;
ALTER TABLE issue ALTER COLUMN team_id DROP NOT NULL;

-- May fail by design; see the WARNING above.
ALTER TABLE issue ADD CONSTRAINT uq_issue_workspace_number UNIQUE (workspace_id, number);
