-- A rebuildable projection and append-only member instructions. No business facts move here.
CREATE TABLE lifeos_secretary_projection (
    workspace_id UUID PRIMARY KEY,
    revision BIGINT NOT NULL DEFAULT 0,
    source_sha256 TEXT NOT NULL DEFAULT '',
    payload JSONB NOT NULL DEFAULT '{}',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE lifeos_secretary_instruction (
    sequence BIGSERIAL PRIMARY KEY,
    workspace_id UUID NOT NULL,
    request_id UUID NOT NULL,
    user_id UUID NOT NULL,
    item_key TEXT NOT NULL,
    kind TEXT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    canonical_receipt TEXT,
    UNIQUE(workspace_id, request_id)
);
