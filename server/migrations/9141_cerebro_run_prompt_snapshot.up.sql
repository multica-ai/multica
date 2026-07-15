-- FIR-3212: byte-exact per-run production prompt snapshot.
--
-- One row per task run, written once by the daemon after it has handed the
-- prompt to the runtime, then never updated. The row records what the runtime
-- actually read (redacted for display), hashes proving what was sent
-- (pre-redaction), and the agent config version that produced it — so a
-- historical run's prompt can never be silently rewritten by later agent edits.
--
-- task_id intentionally carries NO foreign key: task rows are subject to GC,
-- and quality aggregation per agent version must keep the run evidence after
-- the task row is gone. Deleting the agent removes its snapshots.
CREATE TABLE IF NOT EXISTS cerebro_run_prompt_snapshot (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    task_id UUID NOT NULL UNIQUE,
    agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    issue_id UUID,
    -- agent.context_version at CLAIM time (the version whose runtime_config the
    -- daemon actually read), reported by the daemon — not re-resolved at ingest,
    -- where a concurrent version bump could misattribute the run.
    agent_context_version TEXT NOT NULL DEFAULT '',
    -- Resolved from (agent_id, agent_context_version) at ingest when the
    -- version row exists. SET NULL keeps the run evidence if history is pruned.
    agent_context_version_id UUID REFERENCES agent_context_version(id) ON DELETE SET NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    runtime_version TEXT NOT NULL DEFAULT '',
    system_prompt_mode TEXT NOT NULL DEFAULT '',
    -- Ordered layers as delivered to the runtime:
    -- [{name, delivery, byte_size, sha256_original, sha256_redacted, content_redacted}]
    -- delivery is one of: system_prompt | workdir_file | user_prompt.
    -- content_redacted is the display view; original bytes are NOT stored.
    layers JSONB NOT NULL DEFAULT '[]',
    -- SHA-256 over the concatenation of each layer's ORIGINAL content bytes in
    -- order, each layer terminated by a single 0x00 byte. Proves provenance of
    -- what was sent without persisting unredacted content.
    sha256_original TEXT NOT NULL,
    -- Same serialization over the REDACTED layer contents — the hash of what
    -- this row displays. Equal to sha256_original when nothing was redacted.
    sha256_redacted TEXT NOT NULL,
    total_bytes INTEGER NOT NULL DEFAULT 0,
    redacted BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cerebro_run_prompt_snapshot_agent_version
    ON cerebro_run_prompt_snapshot(agent_id, agent_context_version, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_cerebro_run_prompt_snapshot_workspace
    ON cerebro_run_prompt_snapshot(workspace_id, created_at DESC);

-- Snapshots are immutable run evidence: block UPDATE at the database level so
-- no later code path can rewrite history (FIR-3212 hard requirement).
CREATE OR REPLACE FUNCTION cerebro_run_prompt_snapshot_immutable()
RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'cerebro_run_prompt_snapshot rows are immutable (FIR-3212)';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_cerebro_run_prompt_snapshot_immutable
    ON cerebro_run_prompt_snapshot;
CREATE TRIGGER trg_cerebro_run_prompt_snapshot_immutable
    BEFORE UPDATE ON cerebro_run_prompt_snapshot
    FOR EACH ROW EXECUTE FUNCTION cerebro_run_prompt_snapshot_immutable();
