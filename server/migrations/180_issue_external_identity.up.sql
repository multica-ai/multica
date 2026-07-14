DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'uq_issue_workspace_id'
          AND conrelid = 'issue'::regclass
    ) THEN
        ALTER TABLE issue
            ADD CONSTRAINT uq_issue_workspace_id UNIQUE (workspace_id, id);
    END IF;
END
$$;

CREATE TABLE IF NOT EXISTS issue_external_identity (
    workspace_id UUID NOT NULL,
    namespace TEXT NOT NULL,
    external_id TEXT NOT NULL,
    issue_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, namespace, external_id),
    CONSTRAINT issue_external_identity_namespace_check
        CHECK (namespace ~ '^[a-z][a-z0-9_.:-]{0,127}$'),
    CONSTRAINT issue_external_identity_external_id_check
        CHECK (length(external_id) BETWEEN 1 AND 512 AND octet_length(external_id) <= 1024),
    CONSTRAINT issue_external_identity_issue_fk
        FOREIGN KEY (workspace_id, issue_id)
        REFERENCES issue(workspace_id, id)
        ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_issue_external_identity_workspace_issue
    ON issue_external_identity(workspace_id, issue_id);
