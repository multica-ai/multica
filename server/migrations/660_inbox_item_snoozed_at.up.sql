-- MUL-9333: when the snooze on an issue started. The Snoozed view counts the
-- rows that arrived after it ("2 updates while snoozed"). Set together with
-- snoozed_until and cleared with it. Nullable with no default, so the ALTER is
-- a catalog-only change with no table scan and no backfill.
ALTER TABLE inbox_item ADD COLUMN IF NOT EXISTS snoozed_at TIMESTAMPTZ;
