-- CEREBRO-PATCH(company-brain-source-scoped-permissions): FIR-3924 keeps
-- Company Brain source scope on the existing permission row. The nullable
-- columns preserve every ordinary permission unchanged and do not introduce
-- an agent-profile model or seed any rows.

ALTER TABLE cerebro_tool_policy
    ADD COLUMN IF NOT EXISTS company_brain_connection_id UUID,
    ADD COLUMN IF NOT EXISTS company_brain_allowed_read_sources TEXT[],
    ADD COLUMN IF NOT EXISTS company_brain_write_source TEXT,
    ADD COLUMN IF NOT EXISTS company_brain_access_version BIGINT,
    ADD COLUMN IF NOT EXISTS company_brain_lifecycle_state TEXT;

ALTER TABLE cerebro_company_brain_connection
    ADD CONSTRAINT cerebro_company_brain_connection_workspace_id_id_unique
        UNIQUE (workspace_id, id);

ALTER TABLE cerebro_tool_policy
    ADD CONSTRAINT cerebro_tool_policy_company_brain_connection_fk
        FOREIGN KEY (workspace_id, company_brain_connection_id)
        REFERENCES cerebro_company_brain_connection (workspace_id, id)
        ON DELETE CASCADE,
    ADD CONSTRAINT cerebro_tool_policy_company_brain_scope_complete
        CHECK (
            (
                company_brain_connection_id IS NULL
                AND company_brain_allowed_read_sources IS NULL
                AND company_brain_write_source IS NULL
                AND company_brain_access_version IS NULL
                AND company_brain_lifecycle_state IS NULL
            )
            OR
            (
                company_brain_connection_id IS NOT NULL
                AND company_brain_allowed_read_sources IS NOT NULL
                AND company_brain_write_source IS NOT NULL
                AND company_brain_access_version IS NOT NULL
                AND company_brain_lifecycle_state IS NOT NULL
            )
        ),
    ADD CONSTRAINT cerebro_tool_policy_company_brain_scope_agent_grant
        CHECK (
            company_brain_allowed_read_sources IS NULL
            OR (
                layer = 'agent'
                AND setting = 'allow'
                AND resource_pattern = ''
                AND tool_key LIKE 'connection:%'
            )
        ),
    ADD CONSTRAINT cerebro_tool_policy_company_brain_read_sources_valid
        CHECK (
            company_brain_allowed_read_sources IS NULL
            OR (
                cardinality(company_brain_allowed_read_sources) > 0
                AND array_position(company_brain_allowed_read_sources, NULL) IS NULL
                AND array_position(company_brain_allowed_read_sources, '') IS NULL
                AND array_position(
                    company_brain_allowed_read_sources,
                    company_brain_write_source
                ) IS NOT NULL
            )
        ),
    ADD CONSTRAINT cerebro_tool_policy_company_brain_write_source_valid
        CHECK (
            company_brain_write_source IS NULL
            OR btrim(company_brain_write_source) <> ''
        ),
    ADD CONSTRAINT cerebro_tool_policy_company_brain_access_version_valid
        CHECK (
            company_brain_access_version IS NULL
            OR company_brain_access_version > 0
        ),
    ADD CONSTRAINT cerebro_tool_policy_company_brain_lifecycle_known
        CHECK (
            company_brain_lifecycle_state IS NULL
            OR company_brain_lifecycle_state IN ('draft', 'active', 'disabled', 'revoked')
        );
