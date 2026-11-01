-- Company knowledge base (WeKnora sidecar): Multica's registry of what it
-- provisioned, never the documents themselves.
--
-- Primary keys are not declared inline: following 327-329, each is built as a
-- unique index CONCURRENTLY in its own migration (621-623) and attached in 624.
-- Secondary indexes are 625-628. No foreign keys (repo rule): the application
-- removes dependents, and the workspace delete sweep covers all three tables.

-- One row per workspace that enabled knowledge.
CREATE TABLE workspace_knowledge (
    workspace_id UUID NOT NULL,
    weknora_tenant_id TEXT NOT NULL,
    -- The workspace's full-access WeKnora key, sealed with secretbox
    -- (MULTICA_KNOWLEDGE_SECRET_KEY).
    api_key_ciphertext BYTEA NOT NULL,
    state TEXT NOT NULL DEFAULT 'ready' CHECK (state IN ('provisioning', 'ready', 'failed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Multica's view of each knowledge base.
CREATE TABLE knowledge_base (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    weknora_kb_id TEXT NOT NULL,
    key TEXT NOT NULL,
    name TEXT NOT NULL,
    visibility TEXT NOT NULL DEFAULT 'workspace' CHECK (visibility IN ('workspace', 'restricted')),
    position INTEGER NOT NULL DEFAULT 0,
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Where a document came from, and whether it is live.
CREATE TABLE knowledge_link (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    knowledge_base_id UUID NOT NULL,
    weknora_knowledge_id TEXT NOT NULL,
    source_type TEXT NOT NULL CHECK (source_type IN
        ('issue', 'comment', 'proposal', 'brief', 'project', 'upload', 'url', 'sync')),
    source_id UUID,
    idempotency_key TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('draft', 'published', 'failed')),
    created_by_type TEXT NOT NULL CHECK (created_by_type IN ('agent', 'member', 'system')),
    created_by_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ
);
