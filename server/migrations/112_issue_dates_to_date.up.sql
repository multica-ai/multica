-- Issue start_date / due_date are calendar days: a user picks a day (the
-- pickers have no time-of-day input), so "Mar 1" must mean Mar 1 for everyone
-- regardless of timezone. Storing them as TIMESTAMPTZ folded the writer's
-- local midnight into a UTC instant, shifting the displayed day by the local
-- offset in non-UTC timezones (GH #3618 / MUL-2925). DATE carries no time or
-- timezone, so the picked day is preserved as-is.
--
-- Existing rows are truncated at the UTC day boundary, matching what the Gantt
-- already showed for them. `AT TIME ZONE 'UTC'` pins the conversion to UTC
-- explicitly so it does not depend on the migration session's TimeZone setting
-- (a bare `::date` cast would be session-timezone dependent). The original
-- local-day intent of legacy rows is unrecoverable from a bare instant, so this
-- is the best-effort conversion.
ALTER TABLE issue
    ALTER COLUMN start_date TYPE DATE USING (start_date AT TIME ZONE 'UTC')::date,
    ALTER COLUMN due_date TYPE DATE USING (due_date AT TIME ZONE 'UTC')::date;
