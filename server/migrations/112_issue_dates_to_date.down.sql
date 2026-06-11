-- Revert to TIMESTAMPTZ, interpreting each calendar day as UTC midnight
-- explicitly (independent of the session TimeZone): `date::timestamp` yields
-- midnight with no zone, and `AT TIME ZONE 'UTC'` stamps it as the UTC instant.
ALTER TABLE issue
    ALTER COLUMN start_date TYPE TIMESTAMPTZ USING start_date::timestamp AT TIME ZONE 'UTC',
    ALTER COLUMN due_date TYPE TIMESTAMPTZ USING due_date::timestamp AT TIME ZONE 'UTC';
