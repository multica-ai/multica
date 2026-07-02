-- Team numbering cutover ("Migration B").
--
-- Phase 2 of the two-phase team-numbering rollout. Migration 131 added the
-- Team schema additively: issue.team_id / autopilot.team_id are nullable and
-- the legacy uq_issue_workspace_number (UNIQUE(workspace_id, number)) still
-- holds. This migration runs ONLY after every old, team-unaware server
-- instance has drained, so no writer can still emit a NULL team_id.
--
-- It is SAFE-BY-CONSTRUCTION: it re-backfills stragglers, syncs counters
-- upward (never regressing), then VALIDATES the preconditions and RAISEs with
-- actionable detail if anything is off. It never silently "fixes" corrupt
-- data. Only after validation passes does it flip the columns to NOT NULL and
-- swap workspace-scoped uniqueness for team-scoped uniqueness, so that teams
-- number independently from 1 (Linear-style ENG-1, DES-1, ...).
--
-- Steps, in order:
--   1. Re-backfill any issue/autopilot rows an old instance wrote with a NULL
--      team_id during the deploy window to the workspace's default Team, using
--      the same join shape as 131.
--   2. Sync each Team's issue_counter upward to cover the highest number
--      actually minted into it, and — for default Teams — the legacy
--      workspace.issue_counter that old writers advanced during the window.
--      Counters never regress.
--   3. Validate (DO block, RAISE EXCEPTION on failure): zero remaining NULL
--      team_id in issue and autopilot; zero duplicate (team_id, number) pairs
--      in issue. Duplicates are theoretically impossible while
--      uq_issue_workspace_number holds and every issue maps to exactly one
--      Team within its workspace, but we validate anyway and fail loudly
--      rather than mint a broken unique index.
--   4. Cut over: team_id NOT NULL on issue + autopilot; drop
--      uq_issue_workspace_number; add uq_issue_team_number UNIQUE(team_id,
--      number).
--
-- Transaction / locking notes:
--   cmd/migrate executes each .sql file as one implicit transaction (a single
--   conn.Exec). Every statement below is transaction-safe (no CREATE INDEX
--   CONCURRENTLY), so the whole cutover is atomic: any RAISE in step 3 rolls
--   the file back and leaves schema_migrations unchanged for a clean retry.
--   The ALTER TABLE / constraint swap on `issue` briefly takes an ACCESS
--   EXCLUSIVE lock; acceptable at this product's scale and kept short by doing
--   all validation before the locking DDL.
--
-- workspace.issue_prefix / issue_counter are intentionally retained here; a
-- later cleanup migration drops them once nothing reads them (per plan).

-- 1. Re-backfill stragglers written by old instances during the deploy window.
UPDATE issue i
SET team_id = wt.id
FROM workspace_team wt
WHERE wt.workspace_id = i.workspace_id
  AND wt.is_default
  AND i.team_id IS NULL;

UPDATE autopilot a
SET team_id = wt.id
FROM workspace_team wt
WHERE wt.workspace_id = a.workspace_id
  AND wt.is_default
  AND a.team_id IS NULL;

-- 2. Sync counters upward. GREATEST of the current counter, the max number
--    actually minted into the Team, and (default Teams only) the legacy
--    workspace counter old writers incremented. The WHERE guard keeps this to
--    only the rows that would actually rise, so counters never regress.
UPDATE workspace_team wt
SET issue_counter = GREATEST(
        wt.issue_counter,
        COALESCE((SELECT max(i.number) FROM issue i WHERE i.team_id = wt.id), 0),
        CASE WHEN wt.is_default
             THEN COALESCE((SELECT w.issue_counter FROM workspace w WHERE w.id = wt.workspace_id), 0)
             ELSE 0 END
    ),
    updated_at = now()
WHERE wt.issue_counter < GREATEST(
        COALESCE((SELECT max(i.number) FROM issue i WHERE i.team_id = wt.id), 0),
        CASE WHEN wt.is_default
             THEN COALESCE((SELECT w.issue_counter FROM workspace w WHERE w.id = wt.workspace_id), 0)
             ELSE 0 END
    );

-- 3. Validation preflight. Fail loudly with actionable counts/ids; never fix.
DO $$
DECLARE
    null_issue_count integer;
    null_autopilot_count integer;
    dup_count integer;
    offenders text;
BEGIN
    SELECT count(*) INTO null_issue_count FROM issue WHERE team_id IS NULL;
    SELECT count(*) INTO null_autopilot_count FROM autopilot WHERE team_id IS NULL;
    IF null_issue_count > 0 OR null_autopilot_count > 0 THEN
        RAISE EXCEPTION 'team_number_cutover preflight failed: % issue and % autopilot rows still have NULL team_id. An old, team-unaware instance is likely still writing; drain every old instance before running Migration B.',
            null_issue_count, null_autopilot_count;
    END IF;

    SELECT count(*) INTO dup_count
    FROM (
        SELECT team_id, number
        FROM issue
        GROUP BY team_id, number
        HAVING count(*) > 1
    ) d;
    IF dup_count > 0 THEN
        SELECT string_agg(format('(team_id=%s, number=%s, count=%s)', team_id, number, cnt), ', ')
        INTO offenders
        FROM (
            SELECT team_id, number, count(*) AS cnt
            FROM issue
            GROUP BY team_id, number
            HAVING count(*) > 1
            ORDER BY count(*) DESC
            LIMIT 20
        ) s;
        RAISE EXCEPTION 'team_number_cutover preflight failed: % duplicate (team_id, number) pairs exist; refusing to create uq_issue_team_number. Offenders (up to 20): %',
            dup_count, offenders;
    END IF;
END $$;

-- 4. Cutover. Validation above guarantees these succeed.
ALTER TABLE issue ALTER COLUMN team_id SET NOT NULL;
ALTER TABLE autopilot ALTER COLUMN team_id SET NOT NULL;

-- Swap workspace-scoped uniqueness for team-scoped uniqueness. Dropping the
-- constraint drops its backing unique index; the new uq_issue_team_number
-- (team_id, number) serves the only remaining identifier lookup
-- (GetIssueByTeamKeyAndNumber, which joins on team_id then filters number).
-- No production path resolves an issue by (workspace_id, number) after the
-- resolver audit, so no replacement (workspace_id, number) index is added.
ALTER TABLE issue DROP CONSTRAINT uq_issue_workspace_number;
ALTER TABLE issue ADD CONSTRAINT uq_issue_team_number UNIQUE (team_id, number);
