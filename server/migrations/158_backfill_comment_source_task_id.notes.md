# Migration 158 — Backfill `comment.source_task_id`

## What it does

Stamps `source_task_id` on agent-authored comments whose `parent_id` resolves
through `agent_task_queue.trigger_comment_id` to a known task. Mirrors the
back-pointer that the CLI path and the failure-path already wrote.

## When to run

After deploying U1 (commit `feat(tasks): stamp source_task_id on synthesized
completion comment`) which fixes the going-forward write path. This migration
recovers the historical back-pointer. Both must be live before users see the
"View run" affordance resolve on pre-existing comments.

## Pre-flight / monitoring SQL

Run before the migration to capture baseline counts and after to confirm
progress:

```sql
-- 1. Total candidates (these are the rows the migration WILL touch or skip):
SELECT
    COUNT(*) FILTER (
        WHERE source_task_id IS NULL
          AND parent_id IS NOT NULL
          AND author_type = 'agent'
          AND type IN ('comment', 'system')
    ) AS eligible_total,
    COUNT(*) FILTER (
        WHERE source_task_id IS NOT NULL
          AND author_type = 'agent'
    ) AS already_stamped
FROM comment;
```

```sql
-- 2. After migration: how many got filled.
-- Should equal eligible_total minus the 5 skip categories below.
SELECT COUNT(*) FILTER (
    WHERE source_task_id IS NOT NULL
      AND author_type = 'agent'
) AS stamped_after
FROM comment;
```

```sql
-- 3. Still-NULL diagnostics — break down the skip categories.
-- Expect every category to be small relative to total comment volume.
SELECT
    COUNT(*) FILTER (WHERE source_task_id IS NULL AND parent_id IS NULL
        AND author_type = 'agent') AS null_parent,
    COUNT(*) FILTER (WHERE source_task_id IS NULL AND parent_id IS NOT NULL
        AND author_type = 'agent'
        AND NOT EXISTS (
            SELECT 1 FROM agent_task_queue t
            JOIN issue i ON i.id = t.issue_id AND i.workspace_id = c.workspace_id
            WHERE t.trigger_comment_id = c.parent_id
        )) AS trigger_unresolved,
    COUNT(*) FILTER (WHERE source_task_id IS NULL AND parent_id IS NOT NULL
        AND author_type = 'agent'
        AND EXISTS (
            SELECT 1 FROM agent_task_queue t WHERE t.trigger_comment_id = c.parent_id
        )
        AND NOT EXISTS (
            SELECT 1 FROM agent_task_queue t
            JOIN issue i ON i.id = t.issue_id AND i.workspace_id = c.workspace_id
            WHERE t.trigger_comment_id = c.parent_id
        )) AS workspace_mismatch
FROM comment c;
```

```sql
-- 4. Sanity: spot-check 10 backfilled rows resolve to real tasks.
SELECT c.id, c.source_task_id, t.id AS resolved_task_id,
       i.workspace_id AS comment_ws, t.issue_id, i2.workspace_id AS task_ws
FROM comment c
JOIN agent_task_queue t ON t.id = c.source_task_id
JOIN issue i  ON i.id  = c.issue_id
JOIN issue i2 ON i2.id = t.issue_id
WHERE c.source_task_id IS NOT NULL
  AND c.author_type = 'agent'
ORDER BY c.created_at DESC
LIMIT 10;
-- Expect every row to show comment_ws = task_ws (workspace guard holds).
```

## Rollback

`down.sql` is intentionally a no-op (no destructive operation to reverse).
If the forward run failed mid-flight, re-running `up.sql` is safe because the
`WHERE c.source_task_id IS NULL` guard makes the UPDATE a no-op on rows it
already touched.

## Idempotency

Safe to re-run. The `WHERE c.source_task_id IS NULL` predicate means a second
run touches zero rows. Useful after a partial failure or a staging dry-run.

## Workspace guard

The JOIN through `issue` enforces `issue.workspace_id = comment.workspace_id`
on both sides of the agent_task_queue back-pointer. `agent_task_queue` has no
`workspace_id` column (per migration 120's design choice — application layer
resolves the relationship), so this derivation through `issue` is the only
sound way to prevent cross-workspace corruption. A data-integrity violation
(a comment in workspace A whose `parent_id` points to a trigger_comment in
workspace B) would silently stamp a wrong-workspace task without this guard.