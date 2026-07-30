-- CEREBRO-PATCH(company-brain-parity-proof): FIR-3924 stores only the
-- non-secret, versioned equality proof required before an eligible agent can
-- move to one logical Company Brain Connection. This migration creates schema
-- only: it neither classifies agents nor changes Connections, credentials,
-- permissions, approvals, aliases, prompts, agents, or cutover state.

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'cerebro_company_brain_connection_workspace_id_id_unique'
    ) THEN
        ALTER TABLE cerebro_company_brain_connection
            ADD CONSTRAINT cerebro_company_brain_connection_workspace_id_id_unique
            UNIQUE (workspace_id, id);
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'agent_workspace_id_id_unique'
    ) THEN
        ALTER TABLE agent
            ADD CONSTRAINT agent_workspace_id_id_unique
            UNIQUE (workspace_id, id);
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'cerebro_tool_policy_company_brain_parity_identity_unique'
    ) THEN
        ALTER TABLE cerebro_tool_policy
            ADD CONSTRAINT cerebro_tool_policy_company_brain_parity_identity_unique
            UNIQUE (
                workspace_id, id, subject_id,
                company_brain_connection_id, company_brain_access_version
            );
    END IF;
END
$$;

CREATE TABLE IF NOT EXISTS cerebro_company_brain_parity_proof (
    id                          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id                UUID        NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    company_brain_connection_id UUID        NOT NULL,
    target_permission_id        UUID        NOT NULL,
    agent_id                    UUID        NOT NULL,
    census_version              BIGINT      NOT NULL,
    access_version              BIGINT      NOT NULL,
    legacy_access_sha256        TEXT        NOT NULL,
    target_access_sha256        TEXT        NOT NULL,
    legacy_approval_sha256      TEXT        NOT NULL,
    target_approval_sha256      TEXT        NOT NULL,
    legacy_tool_calls_sha256    TEXT        NOT NULL,
    target_tool_calls_sha256    TEXT        NOT NULL,
    legacy_tool_count           INTEGER     NOT NULL,
    target_tool_count           INTEGER     NOT NULL,
    legacy_write_source         TEXT        NOT NULL,
    target_write_source         TEXT        NOT NULL,
    status                      TEXT        NOT NULL,
    blocker_code                TEXT,
    evidence_sha256             TEXT        NOT NULL,
    evidence_at                 TIMESTAMPTZ NOT NULL,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT cerebro_company_brain_parity_proof_identity_unique
        UNIQUE (
            workspace_id, company_brain_connection_id,
            agent_id, census_version
        ),
    CONSTRAINT cerebro_company_brain_parity_proof_connection_fk
        FOREIGN KEY (workspace_id, company_brain_connection_id)
        REFERENCES cerebro_company_brain_connection (workspace_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT cerebro_company_brain_parity_proof_agent_fk
        FOREIGN KEY (workspace_id, agent_id)
        REFERENCES agent (workspace_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT cerebro_company_brain_parity_proof_permission_fk
        FOREIGN KEY (
            workspace_id, target_permission_id, agent_id,
            company_brain_connection_id, access_version
        )
        REFERENCES cerebro_tool_policy (
            workspace_id, id, subject_id,
            company_brain_connection_id, company_brain_access_version
        )
        ON DELETE RESTRICT,
    CONSTRAINT cerebro_company_brain_parity_proof_census_version_valid
        CHECK (census_version > 0),
    CONSTRAINT cerebro_company_brain_parity_proof_access_version_valid
        CHECK (access_version > 0),
    CONSTRAINT cerebro_company_brain_parity_proof_hashes_valid
        CHECK (
            legacy_access_sha256 ~ '^[0-9a-f]{64}$'
            AND target_access_sha256 ~ '^[0-9a-f]{64}$'
            AND legacy_approval_sha256 ~ '^[0-9a-f]{64}$'
            AND target_approval_sha256 ~ '^[0-9a-f]{64}$'
            AND legacy_tool_calls_sha256 ~ '^[0-9a-f]{64}$'
            AND target_tool_calls_sha256 ~ '^[0-9a-f]{64}$'
            AND evidence_sha256 ~ '^[0-9a-f]{64}$'
        ),
    CONSTRAINT cerebro_company_brain_parity_proof_tool_counts_valid
        CHECK (legacy_tool_count > 0 AND target_tool_count > 0),
    CONSTRAINT cerebro_company_brain_parity_proof_sources_valid
        CHECK (
            legacy_write_source ~ '^[a-z0-9][a-z0-9-]{0,63}$'
            AND target_write_source ~ '^[a-z0-9][a-z0-9-]{0,63}$'
        ),
    CONSTRAINT cerebro_company_brain_parity_proof_evidence_time_valid
        CHECK (evidence_at <= clock_timestamp()),
    CONSTRAINT cerebro_company_brain_parity_proof_status_valid
        CHECK (status IN ('matched', 'blocked')),
    CONSTRAINT cerebro_company_brain_parity_proof_blocker_valid
        CHECK (
            blocker_code IS NULL
            OR (
                blocker_code = upper(btrim(blocker_code))
                AND blocker_code ~ '^[A-Z][A-Z0-9]*(-[A-Z0-9]+)+$'
            )
        ),
    CONSTRAINT cerebro_company_brain_parity_proof_exact_match
        CHECK (
            (
                status = 'matched'
                AND blocker_code IS NULL
                AND legacy_access_sha256 = target_access_sha256
                AND legacy_approval_sha256 = target_approval_sha256
                AND legacy_tool_calls_sha256 = target_tool_calls_sha256
                AND legacy_tool_count = target_tool_count
                AND legacy_write_source = target_write_source
            )
            OR (
                status = 'blocked'
                AND blocker_code IS NOT NULL
            )
        )
);

CREATE INDEX IF NOT EXISTS idx_company_brain_parity_proof_cutover
    ON cerebro_company_brain_parity_proof (
        workspace_id, company_brain_connection_id,
        census_version DESC, status
    );

CREATE INDEX IF NOT EXISTS idx_company_brain_parity_proof_agent
    ON cerebro_company_brain_parity_proof (
        workspace_id, agent_id, census_version DESC
    );
