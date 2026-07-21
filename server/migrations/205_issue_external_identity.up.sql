CREATE TABLE issue_external_identity (
    workspace_id UUID NOT NULL,
    namespace TEXT NOT NULL,
    external_id TEXT NOT NULL,
    issue_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT issue_external_identity_namespace_check
        CHECK (namespace ~ '^[a-z][a-z0-9_.:-]{0,127}$'),
    CONSTRAINT issue_external_identity_external_id_check
        CHECK (length(external_id) BETWEEN 1 AND 512 AND octet_length(external_id) <= 1024)
);
