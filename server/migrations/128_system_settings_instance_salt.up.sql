-- Singleton per-deployment settings row. Currently holds the
-- instance_salt used by the self-host onboarding source beacon
-- (server/internal/sourcebeacon): uid_hash = sha256(instance_salt||user_id)
-- and instance_hash = sha256(instance_salt) are computed locally and
-- shipped to Multica's public ingest. The salt never leaves the instance,
-- so Multica cannot reverse a hash back to a user_id, and the same user on
-- two different self-host instances produces different hashes.
--
-- The salt is generated once, here, with pgcrypto's gen_random_bytes
-- (the "pgcrypto" extension is enabled in 001_init). Each deployment that
-- runs this migration gets its own random salt; the official cloud also
-- gets one but never sends a beacon, so its salt is simply unused.
--
-- Single-row table guarded by the id=1 CHECK, mirroring the
-- task_usage_hourly_rollup_state singleton pattern already in the schema.
CREATE TABLE system_settings (
    id            INTEGER     PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    instance_salt TEXT        NOT NULL DEFAULT encode(gen_random_bytes(32), 'hex'),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO system_settings (id) VALUES (1) ON CONFLICT (id) DO NOTHING;
