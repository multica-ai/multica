-- Restore the original 9048 definition: unset/invalid timezones fall back to UTC.
CREATE OR REPLACE FUNCTION cerebro_safe_timezone(tz TEXT) RETURNS TEXT AS $$
BEGIN
    PERFORM now() AT TIME ZONE COALESCE(NULLIF(tz, ''), 'UTC');
    RETURN COALESCE(NULLIF(tz, ''), 'UTC');
EXCEPTION
    WHEN OTHERS THEN
        RETURN 'UTC';
END;
$$ LANGUAGE plpgsql STABLE;
