-- The knowledge domain is independently gated, but its schema is additive so
-- turning the feature off never affects existing issue/chat tables.
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE knowledge_provider (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    name TEXT NOT NULL,
    preset TEXT NOT NULL DEFAULT 'custom',
    protocol TEXT NOT NULL CHECK (protocol IN ('openai_compatible', 'cohere_compatible')),
    base_url TEXT NOT NULL,
    encrypted_api_key BYTEA NOT NULL,
    secret_revision BIGINT NOT NULL DEFAULT 1,
    is_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    revision BIGINT NOT NULL DEFAULT 1,
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE knowledge_model_settings (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    revision BIGINT NOT NULL DEFAULT 1,
    updated_by UUID NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE knowledge_model_binding (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    knowledge_base_id UUID,
    purpose TEXT NOT NULL CHECK (purpose IN ('main', 'extract', 'answer', 'parse', 'embedding', 'rerank')),
    mode TEXT NOT NULL CHECK (mode IN ('inherit', 'explicit', 'auto', 'off')),
    provider_id UUID,
    model TEXT,
    options JSONB NOT NULL DEFAULT '{}',
    revision BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE knowledge_model_capability (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    provider_id UUID NOT NULL,
    secret_revision BIGINT NOT NULL,
    model TEXT NOT NULL,
    capability TEXT NOT NULL CHECK (capability IN ('text', 'structured_output', 'vision', 'embedding', 'rerank')),
    status TEXT NOT NULL CHECK (status IN ('ready', 'unsupported', 'failed', 'not_tested')),
    details JSONB NOT NULL DEFAULT '{}',
    tested_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE knowledge_base (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    creator_id UUID NOT NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    visibility TEXT NOT NULL DEFAULT 'private' CHECK (visibility IN ('private', 'workspace')),
    revision BIGINT NOT NULL DEFAULT 1,
    acl_revision BIGINT NOT NULL DEFAULT 1,
    corpus_revision BIGINT NOT NULL DEFAULT 1,
    active_index_id UUID,
    building_index_id UUID,
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE knowledge_document (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    knowledge_base_id UUID NOT NULL,
    title TEXT NOT NULL,
    source_kind TEXT NOT NULL CHECK (source_kind IN ('file', 'url')),
    source_url TEXT,
    source_identity TEXT NOT NULL,
    tags TEXT[] NOT NULL DEFAULT '{}',
    current_version_id UUID,
    revision BIGINT NOT NULL DEFAULT 1,
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE knowledge_document_version (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    knowledge_base_id UUID NOT NULL,
    document_id UUID NOT NULL,
    version_number BIGINT NOT NULL,
    source_object_key TEXT NOT NULL DEFAULT '',
    source_hash TEXT NOT NULL,
    byte_size BIGINT NOT NULL DEFAULT 0,
    mime_type TEXT NOT NULL DEFAULT 'application/octet-stream',
    source_metadata JSONB NOT NULL DEFAULT '{}',
    parsed_object_key TEXT,
    parser_version TEXT NOT NULL DEFAULT '',
    chunker_version TEXT NOT NULL DEFAULT '',
    config_snapshot JSONB NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'processing' CHECK (status IN ('processing', 'ready', 'unsupported', 'failed', 'cancelled')),
    error_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE knowledge_chunk (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    knowledge_base_id UUID NOT NULL,
    document_id UUID NOT NULL,
    version_id UUID NOT NULL,
    ordinal INT NOT NULL,
    block_refs JSONB NOT NULL DEFAULT '[]',
    text TEXT NOT NULL,
    source_locator JSONB NOT NULL DEFAULT '{}',
    token_estimate INT NOT NULL DEFAULT 0,
    text_hash TEXT NOT NULL,
    keyword_text TEXT NOT NULL DEFAULT '',
    search_vector TSVECTOR NOT NULL DEFAULT to_tsvector('simple', '')
);

CREATE TABLE knowledge_index (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    knowledge_base_id UUID NOT NULL,
    status TEXT NOT NULL DEFAULT 'building' CHECK (status IN ('building', 'active', 'retired', 'failed', 'cancelled')),
    embedding_snapshot JSONB NOT NULL DEFAULT '{}',
    embedding_fingerprint TEXT NOT NULL,
    dimension INT NOT NULL CHECK (dimension BETWEEN 1 AND 4096),
    corpus_revision BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    activated_at TIMESTAMPTZ
);

CREATE TABLE knowledge_embedding (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    knowledge_base_id UUID NOT NULL,
    index_id UUID NOT NULL,
    chunk_id UUID NOT NULL,
    version_id UUID NOT NULL,
    dimension INT NOT NULL CHECK (dimension BETWEEN 1 AND 4096),
    embedding vector NOT NULL
);

CREATE TABLE knowledge_job (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    knowledge_base_id UUID NOT NULL,
    document_version_id UUID,
    index_id UUID,
    stage TEXT NOT NULL CHECK (stage IN ('fetch', 'parse', 'enhance', 'chunk', 'embed', 'extract', 'activate', 'cleanup')),
    shard_key TEXT NOT NULL DEFAULT '',
    config_fingerprint TEXT NOT NULL DEFAULT '',
    logical_key TEXT NOT NULL,
    input JSONB NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'waiting_config', 'succeeded', 'failed', 'cancelled')),
    attempt INT NOT NULL DEFAULT 0,
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_token UUID,
    lease_until TIMESTAMPTZ,
    heartbeat_at TIMESTAMPTZ,
    progress JSONB NOT NULL DEFAULT '{}',
    error_code TEXT,
    result_ref JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE knowledge_entity (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    knowledge_base_id UUID NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('person', 'organization', 'product', 'concept', 'event')),
    canonical_name TEXT NOT NULL,
    normalized_name TEXT NOT NULL,
    identity_key TEXT NOT NULL,
    review_status TEXT NOT NULL DEFAULT 'automatic' CHECK (review_status IN ('automatic', 'confirmed', 'rejected', 'merged', 'historical')),
    revision BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE knowledge_entity_alias (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    knowledge_base_id UUID NOT NULL,
    entity_id UUID NOT NULL,
    normalized_alias TEXT NOT NULL,
    disambiguator TEXT NOT NULL DEFAULT '',
    provenance JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE knowledge_relation (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    knowledge_base_id UUID NOT NULL,
    source_entity_id UUID NOT NULL,
    target_entity_id UUID NOT NULL,
    predicate TEXT NOT NULL CHECK (predicate IN ('is_a', 'part_of', 'belongs_to', 'creates', 'uses', 'depends_on', 'causes', 'supports', 'contradicts', 'related_to')),
    qualifier JSONB NOT NULL DEFAULT '{}',
    identity_key TEXT NOT NULL,
    review_status TEXT NOT NULL DEFAULT 'automatic' CHECK (review_status IN ('automatic', 'confirmed', 'rejected', 'historical')),
    revision BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE knowledge_evidence (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    knowledge_base_id UUID NOT NULL,
    subject_type TEXT NOT NULL CHECK (subject_type IN ('entity', 'relation')),
    subject_id UUID NOT NULL,
    version_id UUID NOT NULL,
    chunk_id UUID NOT NULL,
    source_locator JSONB NOT NULL DEFAULT '{}',
    quote TEXT NOT NULL,
    quote_hash TEXT NOT NULL,
    start_offset INT,
    end_offset INT,
    extraction_run_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE knowledge_extraction_run (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    knowledge_base_id UUID NOT NULL,
    version_id UUID NOT NULL,
    config_fingerprint TEXT NOT NULL,
    schema_version TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'succeeded', 'failed', 'cancelled')),
    is_active BOOLEAN NOT NULL DEFAULT FALSE,
    stats JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    activated_at TIMESTAMPTZ
);

CREATE TABLE knowledge_graph_edit (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    knowledge_base_id UUID NOT NULL,
    operation TEXT NOT NULL CHECK (operation IN ('rename', 'retype', 'merge', 'reject_relation', 'edit_relation', 'confirm')),
    target_id UUID NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}',
    previous_state JSONB NOT NULL DEFAULT '{}',
    actor_id UUID NOT NULL,
    expected_revision BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    reverted_at TIMESTAMPTZ
);

CREATE TABLE knowledge_request (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    actor_key TEXT NOT NULL,
    operation TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('processing', 'succeeded', 'failed')),
    dependency_refs JSONB NOT NULL DEFAULT '{}',
    result_ref JSONB NOT NULL DEFAULT '{}',
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
