-- Fork-specific (FIR-2412 follow-up): the day-of reminder sweep computes "today"
-- in the assignee's IANA timezone (user.timezone). That column is NULL for most
-- users — it only gets set when someone overrides their report-viewing tz — and
-- the original cerebro_safe_timezone() fell back to 'UTC' in that case. Firtal is
-- a Scandinavian company, so a UTC day-boundary makes "Due today" land on the
-- wrong calendar day for the late-evening window (CET/CEST is UTC+1/+2). All
-- Scandinavian zones share Copenhagen's offset, so 'Europe/Copenhagen' is the
-- correct default when the user has not picked a timezone. Invalid stored values
-- fall back to the same default rather than UTC. Set timezones pass through
-- unchanged. CREATE OR REPLACE so this re-defines the function in place.
CREATE OR REPLACE FUNCTION cerebro_safe_timezone(tz TEXT) RETURNS TEXT AS $$
BEGIN
    PERFORM now() AT TIME ZONE COALESCE(NULLIF(tz, ''), 'Europe/Copenhagen');
    RETURN COALESCE(NULLIF(tz, ''), 'Europe/Copenhagen');
EXCEPTION
    WHEN OTHERS THEN
        RETURN 'Europe/Copenhagen';
END;
$$ LANGUAGE plpgsql STABLE;
