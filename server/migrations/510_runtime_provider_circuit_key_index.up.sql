-- One circuit row per (runtime_id, provider): this is the serialization key and
-- the ON CONFLICT target for the terminal failure/success upserts.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_runtime_provider_circuit_key ON runtime_provider_circuit (runtime_id, provider);
