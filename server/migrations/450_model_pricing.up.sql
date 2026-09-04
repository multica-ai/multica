CREATE TABLE model_pricing_catalog (
    id BOOLEAN NOT NULL DEFAULT TRUE CHECK (id),
    document JSONB NOT NULL DEFAULT '{}',
    checked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    succeeded_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT ''
);

CREATE TABLE workspace_model_pricing (
    workspace_id UUID NOT NULL,
    overrides JSONB NOT NULL DEFAULT '{}',
    revision BIGINT NOT NULL DEFAULT 1,
    updated_by UUID NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
