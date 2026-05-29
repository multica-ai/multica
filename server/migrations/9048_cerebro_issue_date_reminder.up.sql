-- Fork-specific: tracks which start/due-date reminders have already fired so
-- the sweeper rings exactly once per issue + kind + calendar day. A new row is
-- claimed atomically (INSERT ... ON CONFLICT DO NOTHING) when the date arrives.
-- Changing an issue's date produces a new reminder_date and therefore a fresh
-- reminder on the new day.
CREATE TABLE IF NOT EXISTS cerebro_issue_date_reminder (
    issue_id      UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    kind          TEXT NOT NULL CHECK (kind IN ('start', 'due')),
    reminder_date DATE NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, kind, reminder_date)
);

-- Returns a timezone name safe to feed to `AT TIME ZONE`, normalising empty /
-- NULL values to 'UTC' and falling back to 'UTC' when the stored value is not a
-- timezone this Postgres server recognises. Without this guard a single "user"
-- row carrying a malformed timezone string raises inside the reminder sweep's
-- `AT TIME ZONE` cast and rolls back the whole statement — silencing reminders
-- for *every* user that tick, not just the one with bad data. STABLE: the result
-- depends only on the argument and the server's (stable) timezone catalog.
CREATE OR REPLACE FUNCTION cerebro_safe_timezone(tz TEXT) RETURNS TEXT AS $$
BEGIN
    PERFORM now() AT TIME ZONE COALESCE(NULLIF(tz, ''), 'UTC');
    RETURN COALESCE(NULLIF(tz, ''), 'UTC');
EXCEPTION
    WHEN OTHERS THEN
        RETURN 'UTC';
END;
$$ LANGUAGE plpgsql STABLE;
