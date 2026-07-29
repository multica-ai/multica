-- CEREBRO-PATCH(company-brain-logical-connection): FIR-3924 one catalog-owning
-- Company Brain connection per workspace. The referenced workspace_connection
-- remains the single storage location for URL, tools, instructions, and
-- scopable_args; this table adds identity and contract integrity without
-- copying those fields or changing existing connection rows.

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'workspace_connection_workspace_id_id_unique'
    ) THEN
        ALTER TABLE workspace_connection
            ADD CONSTRAINT workspace_connection_workspace_id_id_unique
            UNIQUE (workspace_id, id);
    END IF;
END
$$;

CREATE TABLE IF NOT EXISTS cerebro_company_brain_connection (
    id                   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id         UUID        NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    connection_id        UUID        NOT NULL,
    tool_contract_sha256 TEXT        NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT cerebro_company_brain_connection_workspace_unique
        UNIQUE (workspace_id),
    CONSTRAINT cerebro_company_brain_connection_connection_unique
        UNIQUE (connection_id),
    CONSTRAINT cerebro_company_brain_connection_workspace_connection_fk
        FOREIGN KEY (workspace_id, connection_id)
        REFERENCES workspace_connection (workspace_id, id)
        ON DELETE CASCADE,
    CONSTRAINT cerebro_company_brain_connection_contract_hash_valid
        CHECK (tool_contract_sha256 ~ '^[0-9a-f]{64}$')
);
