-- I4127.DP / I4192.DP: an issue whose EFFECTIVE status is in_progress must
-- have an assignee.
--
-- WillEnqueueRun only starts a run for a valid assignee. An issue in the
-- in_progress category without one is a zombie: in_progress forever, no run,
-- no comments, no owner, while still counting against the queue ceiling.
-- The application layer enforces this in the HTTP handler (CreateIssue,
-- UpdateIssue, BatchUpdateIssues) and in IssueService.Create, but those only
-- cover the transports that reach them. This CHECK backs them up at the last
-- line of defense, so no raw-SQL write path (backfill, admin tooling, future
-- entry points) can mint or leave a zombie either.
--
-- The check uses issue_effective_status (migration 340), the SQL mirror of
-- issuestatus.Effective: a built-in key short-circuits with no catalog
-- lookup, and a custom status resolves to the canonical key whose behavior
-- it inherits, so a custom in_progress-category status is held to the same
-- rule as the built-in (MUL-6243). The function is STABLE, so the check
-- follows the live catalog: if an admin recategorizes a status away from
-- in_progress, the constraint stops applying on the next write.
--
-- Existing rows are repaired first: any legacy zombie is moved to backlog —
-- the documented remediation from the 18 aug 2026 cleanup (I4127.DP), not a
-- data-destroying rewrite. The count is surfaced as a NOTICE so the operator
-- sees how many rows the migration touched. The constraint is then added with
-- a full validation scan; after the repair no row can violate it.
DO $$
DECLARE
    repaired integer;
BEGIN
    UPDATE issue
       SET status = 'backlog',
           updated_at = now()
     WHERE issue_effective_status(workspace_id, status) = 'in_progress'
       AND (assignee_type IS NULL OR assignee_id IS NULL);
    GET DIAGNOSTICS repaired = ROW_COUNT;
    IF repaired > 0 THEN
        RAISE NOTICE 'issue_in_progress_requires_assignee: repaired % legacy zombie issue(s) to backlog', repaired;
    END IF;
END $$;

ALTER TABLE issue
    ADD CONSTRAINT issue_in_progress_requires_assignee_check
    CHECK (
        issue_effective_status(workspace_id, status) <> 'in_progress'
        OR (assignee_type IS NOT NULL AND assignee_id IS NOT NULL)
    );
