-- 9145_cerebro_availability_evidence: what a capability can ACTUALLY do on a
-- runtime, as opposed to what configuration claims.
--
-- The schema mirrors server/internal/cerebro/availabilityevidence exactly. It
-- attaches to the canonical capability IDs minted in 9144 by foreign key, so a
-- claim can never be recorded against a name the catalog does not know.
--
-- Additive and inert: this table records observations. It grants nothing, denies
-- nothing, and no access decision reads it. The existing runtime self-report
-- path (cerebro_capability_alias, key_source='runtime_report') is untouched.

CREATE TABLE IF NOT EXISTS cerebro_availability_evidence (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    capability_id TEXT NOT NULL
        REFERENCES cerebro_canonical_capability(canonical_id) ON DELETE CASCADE,
    -- The runtime family the evidence was observed on. Evidence is never global:
    -- the same capability can be real on one runtime and absent on another, and
    -- that difference is the reason this table exists.
    runtime_type TEXT NOT NULL,
    -- declared   — configuration says so. Grants nothing, proves nothing.
    -- discovered — a probe found it on the runtime's live surface.
    -- verified   — a test call proved BOTH access and refusal.
    -- Only 'verified' may be presented as reality.
    level TEXT NOT NULL,
    -- Why the capability earned no more than it did. Always populated so a
    -- reader never has to guess why something is unproven.
    reason TEXT NOT NULL,
    -- The recorded test calls behind the level, as
    -- [{subject, authorized, outcome, detail, observed_at}]. Empty for declared.
    proofs JSONB NOT NULL DEFAULT '[]'::jsonb,
    observed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT cerebro_availability_evidence_runtime_known CHECK (runtime_type IN (
        'firtal-gateway', 'claude-code', 'local'
    )),
    CONSTRAINT cerebro_availability_evidence_level_known CHECK (level IN (
        'declared', 'discovered', 'verified'
    )),
    CONSTRAINT cerebro_availability_evidence_reason_not_blank
        CHECK (length(trim(reason)) > 0),
    CONSTRAINT cerebro_availability_evidence_proofs_is_array
        CHECK (jsonb_typeof(proofs) = 'array'),
    -- 'verified' is the only level presented as reality, so it may never rest on
    -- an empty proof set. The database refuses to store an unproven verification
    -- even if a future caller asks it to.
    CONSTRAINT cerebro_availability_evidence_verified_needs_proof
        CHECK (level <> 'verified' OR jsonb_array_length(proofs) >= 2),
    -- One current verdict per capability per runtime. A re-probe overwrites its
    -- row; the table reports the present truth, never a pile of stale claims.
    CONSTRAINT cerebro_availability_evidence_one_per_runtime
        UNIQUE (capability_id, runtime_type)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_availability_evidence_level
    ON cerebro_availability_evidence (level, runtime_type, capability_id);

-- The self-lookup (FIR-3398) recorded per runtime family. It is seeded at
-- 'declared' on purpose: this migration is configuration, and configuration is
-- exactly the thing that cannot prove anything. Only a probe against the live
-- runtime surface may raise these rows to discovered or verified.
INSERT INTO cerebro_canonical_capability
    (canonical_id, family, source_reference)
VALUES
    ('platform:get_agent_capabilities', 'platform',
        'server/internal/cerebro/capabilitycatalog/catalog.go')
ON CONFLICT (canonical_id) DO NOTHING;

INSERT INTO cerebro_capability_alias
    (capability_id, surface, provider, key_value, resource_pattern, key_source, relation, source_reference)
VALUES
    ('platform:get_agent_capabilities', 'policy', '', 'get_agent_capabilities', '', 'platform', 'canonical',
        'server/internal/cerebro/clitools/mcp_tools_agent_capabilities.go'),
    ('platform:get_agent_capabilities', 'bridge', '', 'get_agent_capabilities', '', '', 'alias',
        'server/internal/cerebro/runtime/firtal_gateway_agent_capabilities.go')
ON CONFLICT (surface, provider, key_value, resource_pattern, key_source) DO NOTHING;

INSERT INTO cerebro_availability_evidence
    (capability_id, runtime_type, level, reason)
SELECT 'platform:get_agent_capabilities', rt, 'declared',
       'seeded from configuration; no probe has run against this runtime yet'
FROM unnest(ARRAY['firtal-gateway', 'claude-code', 'local']) AS rt
ON CONFLICT (capability_id, runtime_type) DO NOTHING;
