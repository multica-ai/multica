---
title: "Run-comment View run button — deployment SOP, monitoring SQL, rollback"
date: 2026-07-13
category: "workflow-issues"
module: "comments"
problem_type: "workflow_issue"
component: "deployment_workflow"
severity: "medium"
applies_when:
  - "Deploying the run-comment View run button feature (commits 1c79939..HEAD on feat/run-comment-view-run-button)"
  - "Running migration 158 (backfill comment.source_task_id)"
  - "Operating the comment → run → task transcript rendering pipeline"
  - "Investigating why a comment shows the View run chip or the GC hover-card explainer"
tags: [deployment, runbook, backfill, source_task_id, view-run, migration-158, self-hosted, post-upgrade]
---

# Run-comment View run button — deployment runbook

## Context

This runbook covers the production rollout of the "View run" affordance
on issue comments. The change spans four commits on
`feat/run-comment-view-run-button` (relative to the branch base):

1. `feat(tasks): stamp source_task_id on synthesized completion comment`
   — U1. Backend: `task.go` completion path now passes `task.ID` as the
   synthesized-success comment's `source_task_id`, mirroring the failure
   path. Includes a DB-backed Go regression test (skips without DB).
2. `feat(comments): add migration 158 to backfill source_task_id on
   historical comments` — U2. SQL migration that backfills
   `source_task_id` on historical agent-authored comments whose
   `parent_id` resolves through `agent_task_queue.trigger_comment_id`,
   with a `JOIN issue` workspace guard.
3. `feat(comments): surface a View run chip on agent comments whose
   source_task_id resolves` — U3. Frontend: a new sync cache hook and
   inline chip in the comment card that opens the existing transcript
   dialog.
4. `feat(comments): surface a hover-card explainer when the source run
   is missing` — U4. When `source_task_id` is set but the task is
   unresolvable from cache, a small hover-trigger surfaces an
   explanation.

The frontend cache contract is `issueKeys.tasks(issueId)`, populated
by `listTasksByIssue` and invalidated by the realtime hub's WS
`task:*` aggregator on a 750ms debounce (see
`use-realtime-sync.ts:920-927`). The chip is purely a cache read — no
new endpoint, no new query.

## Rollout sequence

The two changes (U1 going-forward stamp + U2 backfill) must deploy
together. U1 alone leaves historical rows unbackfilled; U2 alone
recovers historical data but the going-forward write path is still
broken. Rollback independence:

- U1 (backend code) is safe to revert after deployment — backfilled
  rows keep their stamp; new comments simply revert to the empty
  stamp until U1 ships again.
- U2 (migration) is forward-only — its `down.sql` is a no-op. A
  partial backfill can be re-run safely (the UPDATE is idempotent).
- U3 / U4 (frontend) can deploy independently of U1+U2; the chip
  simply won't appear on unbackfilled rows.

## Step-by-step rollout

### 1. Pre-flight (staging)

```bash
# Build the new frontend + backend.
cd /path/to/multica
pnpm install
make build   # full pipeline per project README
```

Deploy to staging, then run the monitoring SQL (see below) to
capture the baseline counts. Confirm baseline before promoting to
production — if the staging counts differ from the production
baseline, something is wrong with the staging data.

### 2. Production code deploy (U1 + U3 + U4)

Deploy the application code without running migration 158 yet. This
ships the going-forward stamp (U1) and the UI (U3/U4) without
backfilled data, so users see:

- The View run chip on every comment written from this deploy
  forward.
- No chip on historical comments (the backfill hasn't run).
- The GC hover-card explainer on historical comments where
  `source_task_id` exists in the row but no AgentTask is resolvable
  in cache (this is the expected pre-backfill state for migrated
  columns; should be near-zero unless the original CLI path had
  already written `source_task_id`).

### 3. Migration 158 (U2) — dry run

Run the migration in a transaction with `BEGIN; ... ROLLBACK;` on
staging first. Compare the affected-row count with the staging
baseline. If the delta is reasonable (within an order of magnitude
of the pre-migration count of NULL `source_task_id` rows), proceed.

For a non-transactional dry run on production, see the baseline +
skip-category monitoring SQL below. The migration is idempotent and
safe to re-run.

### 4. Production migration 158 (U2)

Run the migration during a low-traffic window. The migration is a
single UPDATE with a `WHERE c.source_task_id IS NULL` guard, so it is
idempotent. Monitor progress with the migration notes' SQL
(`server/migrations/158_backfill_comment_source_task_id.notes.md`).

### 5. Post-migration verification

Run the post-migration monitoring SQL (see below) to confirm:

- `eligible_total` drops to 0 (or near-zero: residual NULLs are
  comments whose `parent_id` doesn't resolve, which is expected).
- The 10-row spot-check from the migration notes shows
  `comment_ws = task_ws` for every row (workspace guard held).
- No new errors in the frontend Sentry / browser console related to
  `ViewRunButton` or `useAgentTaskForComment`.

## Monitoring SQL

```sql
-- A. Volume: how many comments now have a source_task_id.
-- Expect substantial increase after the migration. Pre-migration
-- baseline should be captured in step 1.
SELECT
    COUNT(*) FILTER (WHERE source_task_id IS NOT NULL) AS stamped_total,
    COUNT(*) FILTER (WHERE source_task_id IS NULL
                       AND author_type = 'agent') AS still_null_agent,
    COUNT(*) FILTER (WHERE author_type = 'agent') AS agent_total
FROM comment;
```

```sql
-- B. The View run chip's view-model contract: every stamped
-- comment's source_task_id must resolve to an existing
-- agent_task_queue row in the same workspace.
-- Expect every row to show comment_ws = task_ws. Any mismatch is
-- a workspace-guard failure and must be remediated.
SELECT
    c.id AS comment_id,
    c.source_task_id,
    t.id AS task_id,
    i_c.workspace_id AS comment_ws,
    i_t.workspace_id AS task_ws
FROM comment c
JOIN agent_task_queue t ON t.id = c.source_task_id
JOIN issue i_c ON i_c.id = c.issue_id
JOIN issue i_t ON i_t.id = t.issue_id
WHERE c.author_type = 'agent'
  AND c.source_task_id IS NOT NULL
  AND i_c.workspace_id <> i_t.workspace_id
LIMIT 20;
-- Expected: 0 rows. Any non-zero result indicates cross-workspace
-- data corruption and must be remediated by re-running the
-- migration with a manual workspace-join predicate.
```

```sql
-- C. The still-NULL skip categories, by reason. Together these
-- should sum to roughly the still_null_agent count from query A.
-- The five skip categories are documented in the migration notes.
-- Run as a single query for a side-by-side breakdown.
SELECT
    COUNT(*) FILTER (WHERE c.source_task_id IS NULL
                       AND c.parent_id IS NULL
                       AND c.author_type = 'agent') AS skip_null_parent,
    COUNT(*) FILTER (WHERE c.source_task_id IS NULL
                       AND c.parent_id IS NOT NULL
                       AND c.author_type = 'agent'
                       AND NOT EXISTS (
                           SELECT 1 FROM agent_task_queue t
                           JOIN issue i ON i.id = t.issue_id
                                       AND i.workspace_id = c.workspace_id
                           WHERE t.trigger_comment_id = c.parent_id
                       )) AS skip_trigger_unresolved,
    COUNT(*) FILTER (WHERE c.source_task_id IS NULL
                       AND c.parent_id IS NOT NULL
                       AND c.author_type = 'agent'
                       AND EXISTS (
                           SELECT 1 FROM agent_task_queue t
                           WHERE t.trigger_comment_id = c.parent_id
                       )
                       AND NOT EXISTS (
                           SELECT 1 FROM agent_task_queue t
                           JOIN issue i ON i.id = t.issue_id
                                       AND i.workspace_id = c.workspace_id
                           WHERE t.trigger_comment_id = c.parent_id
                       )) AS skip_workspace_mismatch
FROM comment c;
```

```sql
-- D. GC explainer traffic. The hover-card explainer is rendered
-- only when source_task_id is set but the AgentTask cannot be
-- resolved from cache. This query is a proxy: count historical
-- rows whose source_task_id exists in the DB but points to a
-- missing or workspace-mismatched AgentTask — these are the
-- comments the explainer will fire on.
SELECT COUNT(*)
FROM comment c
WHERE c.author_type = 'agent'
  AND c.source_task_id IS NOT NULL
  AND (
      NOT EXISTS (
          SELECT 1 FROM agent_task_queue t
          JOIN issue i ON i.id = t.issue_id
                       AND i.workspace_id = c.workspace_id
          WHERE t.id = c.source_task_id
      )
      OR EXISTS (
          SELECT 1 FROM agent_task_queue t
          JOIN issue i ON i.id = t.issue_id
          WHERE t.id = c.source_task_id
            AND i.workspace_id <> c.workspace_id
      )
  );
-- Expected: 0. Any non-zero value represents a real user-visible
-- "the chip is missing" surface; investigate before user reports.
```

## Frontend behavior verification

```bash
# After both code and migration are live, run the e2e suites the
# plan reserves for U5 (placeholders; create alongside when the
# staging environment is ready):
#   e2e/issues/run-view-button.spec.ts
#   e2e/issues/run-view-button-backfill.spec.ts

# Local regression (already green pre-deploy):
cd packages/views && pnpm test    # 1797 vitest cases
cd /Users/fengzhao/multica && pnpm typecheck
cd /Users/fengzhao/multica && pnpm lint
cd server && go test ./internal/service/... -count=1
cd server && go test -count=1 ./cmd/server/ -run "SourceTaskID"   # DB-backed, skips without DATABASE_URL
```

The e2e specs are deliberately not authored in this runbook's scope:
they require a running stack (Postgres + dev server + seeded user)
that a single self-hosted operator does not have on a developer
laptop. The hook and component tests in the package are the unit-
level regression guard.

## Rollback

### Backend code (U1)

Revert the `feat(tasks): stamp source_task_id...` commit. New
comments revert to the empty stamp; backfilled comments keep their
stamp. No data loss; the UI falls back to the GC explainer for new
comments whose `source_task_id` is empty.

### Frontend code (U3 / U4)

Revert the two `feat(comments): ...` commits. Comments render as
they did before the feature (no chip, no explainer). No data impact.

### Migration 158 (U2)

`158_backfill_comment_source_task_id.down.sql` is intentionally a
`SELECT 1` (no-op). Backfill cannot be rolled back without an
audit table, and the migration is idempotent: re-running the
`up.sql` will fill any rows the previous run missed (for example,
rows whose `parent_id` was GC'd before the previous run and not
yet cleaned up). To "soft-rollback", manually NULL out the affected
column on rows that shouldn't have been filled:

```sql
-- Only run if the backfill produced provably wrong rows (e.g.
-- cross-workspace leakage per query B above). This undoes the
-- backfill for those rows; the going-forward stamp (U1) keeps
-- writing the correct value for new comments.
UPDATE comment c
SET source_task_id = NULL
WHERE c.author_type = 'agent'
  AND c.source_task_id IS NOT NULL
  AND NOT EXISTS (
      SELECT 1 FROM agent_task_queue t
      JOIN issue i ON i.id = t.issue_id
                   AND i.workspace_id = c.workspace_id
      WHERE t.id = c.source_task_id
  );
```

This is a destructive action; only run it after confirming query B
reports non-zero rows.

## Known deferred items (not in this feature's scope)

- **issue_child_done.go system-comment backfill**: The
  `issue_child_done.go` system comments (parent-noted "child done"
  notifications) have no `source_task_id` and were excluded from
  the migration. They are not agent-run products. Surfacing a View
  run affordance on them is a separate product decision (does a
  parent-issue child-done notification have a "run" to view?).
  Tracked under `Deferred for later` in the plan's Scope Boundaries.

- **Workspace guard verification per deployment**: Query B
  verifies the workspace guard held. The guard depends on
  `issue.workspace_id` being correctly populated — this is a
  pre-existing data assumption (the `comment` table's
  `workspace_id` column is backfilled at issue creation time per
  migration 025). If your deployment has comment rows with
  NULL `workspace_id` (pre-migration-025), the migration is
  defensive but the workspace guard cannot help. Run a one-off
  audit before this rollout.

- **e2e specs**: Author `e2e/issues/run-view-button.spec.ts` and
  `e2e/issues/run-view-button-backfill.spec.ts` in a follow-up
  PR when the staging environment supports Playwright with a
  seeded user.