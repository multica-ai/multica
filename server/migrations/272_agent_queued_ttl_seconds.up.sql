ALTER TABLE agent
    ADD COLUMN IF NOT EXISTS queued_ttl_seconds DOUBLE PRECISION;

ALTER TABLE agent
    DROP CONSTRAINT IF EXISTS agent_queued_ttl_seconds_valid;

ALTER TABLE agent
    ADD CONSTRAINT agent_queued_ttl_seconds_valid
    CHECK (
        queued_ttl_seconds IS NULL
        OR (
            queued_ttl_seconds > 0
            AND queued_ttl_seconds::text <> 'NaN'
            AND queued_ttl_seconds NOT IN ('Infinity'::double precision, '-Infinity'::double precision)
        )
    );
