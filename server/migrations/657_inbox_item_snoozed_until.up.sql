-- MUL-9215: when a member snoozes an issue in the Inbox, every active row they
-- have on it carries the time it comes back. NULL means not snoozed, so
-- existing rows need no backfill and older servers keep writing valid rows.
-- The column is nullable with no default, which keeps the ALTER a catalog-only
-- change with no table scan.
ALTER TABLE inbox_item ADD COLUMN IF NOT EXISTS snoozed_until TIMESTAMPTZ;
