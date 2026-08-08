CREATE TABLE platform_extension_release (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    extension_key TEXT NOT NULL,
    name TEXT NOT NULL,
    version TEXT NOT NULL,
    digest TEXT NOT NULL,
    manifest JSONB NOT NULL,
    runtime_id UUID NULL,
    squad_id UUID NULL,
    resources JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
