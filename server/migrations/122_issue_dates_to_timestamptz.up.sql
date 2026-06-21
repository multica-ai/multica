-- Widen issue scheduling columns from DATE to TIMESTAMPTZ so a time-of-day can
-- be stored. The API still reads/writes date-only until a follow-up change;
-- this is a pure type widening. 0 rows currently carry a date, so the USING
-- conversion is effectively a no-op. It is pinned to UTC (start_date::timestamp
-- AT TIME ZONE 'UTC') rather than a bare cast so the result does not depend on
-- the migration session's TimeZone setting -- mirroring migration 112.
ALTER TABLE issue
    ALTER COLUMN start_date TYPE TIMESTAMPTZ USING (start_date::timestamp AT TIME ZONE 'UTC'),
    ALTER COLUMN due_date   TYPE TIMESTAMPTZ USING (due_date::timestamp   AT TIME ZONE 'UTC');
