-- Add `updated_at` to task_usage so the daily-rollup worker (added in 073)
-- can detect rows that were corrected by `UpsertTaskUsage` after their
-- original creation. The existing UPSERT path overwrites token counts on
-- conflict but leaves created_at unchanged, so a watermark on created_at
-- alone would silently miss those corrections.
--
-- Schema-only, online-safe migration. The column is nullable with no
-- backfill UPDATE so this is metadata-only on a hot, high-write table —
-- no full-table rewrite, no row-lock storm, no WAL spike. Old rows stay
-- NULL; the rollup function (073) handles them via
-- `COALESCE(updated_at, created_at)` and an OR branch in the window
-- filter so legacy rows are still discoverable by backfill.
--
-- DEFAULT now() is set after the column exists so new INSERTs (and
-- UpsertTaskUsage on conflict, which sets the value explicitly) always
-- get a timestamp. Setting the default on an existing column does NOT
-- touch existing rows; only new rows get the default. This keeps the
-- migration cheap on ~hundreds of millions of `task_usage` rows.
ALTER TABLE task_usage
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ;

ALTER TABLE task_usage
    ALTER COLUMN updated_at SET DEFAULT now();
