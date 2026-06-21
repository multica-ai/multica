-- Revert issue scheduling columns to DATE. Truncates any time-of-day at the UTC
-- day boundary (matching migration 112's conversion). AT TIME ZONE 'UTC' pins
-- the conversion so it does not depend on the session TimeZone.
ALTER TABLE issue
    ALTER COLUMN start_date TYPE DATE USING (start_date AT TIME ZONE 'UTC')::date,
    ALTER COLUMN due_date   TYPE DATE USING (due_date   AT TIME ZONE 'UTC')::date;
