-- FIR-2283 v2 point 8 — "per-issue workflow activation". A saved issue_loop
-- recipe (cerebro_workflow.workflow_type = 'issue_loop') is a REUSABLE
-- template: the same recipe can be picked on many different issues, each
-- running its own independent loop. Compile already scopes each activation's
-- rules to one issue via an issue.id/eq condition (see
-- loops.CompileParams.IssueID, FIR-2283 v2 slice 2b) — this migration adds
-- the column that lets the materialized child rows for TWO DIFFERENT issues
-- coexist without colliding.
--
-- Without this column, IssueLoopBridge.SyncIssueLoop's delete-then-recreate
-- strategy (DeleteGeneratedChildren, keyed only by generated_from_workflow_id)
-- would wipe issue A's compiled rules the moment the SAME recipe is
-- activated on issue B, since both sets of generated rows share the same
-- parent. generated_for_issue_id splits that: NULL means "the project-wide
-- compile of this recipe" (the existing Create/Update-recipe path,
-- unchanged); a real issue id means "the rules compiled for activating this
-- recipe on THIS ONE issue" — so DeleteGeneratedChildren can be scoped to
-- (parentID, issueID) and never touch another issue's activation.
--
-- No separate "issue -> workflow" binding table is needed: the binding is
-- fully derivable from the materialized rows themselves — "which recipe is
-- issue X running" is `SELECT DISTINCT generated_from_workflow_id FROM
-- cerebro_workflow WHERE generated_for_issue_id = X`.
ALTER TABLE cerebro_workflow
    ADD COLUMN IF NOT EXISTS generated_for_issue_id UUID REFERENCES issue(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS idx_cerebro_workflow_generated_for_issue
    ON cerebro_workflow (generated_for_issue_id)
    WHERE generated_for_issue_id IS NOT NULL;

-- One recipe activated at most once per issue: re-activating replaces (via
-- delete-then-recreate), it never appends a second parallel set of rules for
-- the same (recipe, issue) pair.
CREATE UNIQUE INDEX IF NOT EXISTS idx_cerebro_workflow_activation_unique
    ON cerebro_workflow (generated_from_workflow_id, generated_for_issue_id, name)
    WHERE generated_for_issue_id IS NOT NULL;
