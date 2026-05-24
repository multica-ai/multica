-- 9037_cerebro_capability_register: workspace capability register.
--
-- Fase 1: persist a normalized registry of capabilities and the subjects
-- that report, own, or use them. The source-of-truth discovery payload stays
-- in Multica core; Persona may consume this later, but this schema has no
-- Persona dependency.

CREATE TABLE IF NOT EXISTS cerebro_capability (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    capability_key TEXT NOT NULL,
    title TEXT NOT NULL,
    category TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT 'report',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_reported_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT cerebro_capability_key_not_blank CHECK (length(trim(capability_key)) > 0),
    CONSTRAINT cerebro_capability_title_not_blank CHECK (length(trim(title)) > 0),
    CONSTRAINT cerebro_capability_category_not_blank CHECK (length(trim(category)) > 0),
    CONSTRAINT cerebro_capability_unique UNIQUE (workspace_id, capability_key)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_capability_workspace_category
    ON cerebro_capability (workspace_id, category, lower(title));

CREATE TABLE IF NOT EXISTS cerebro_capability_subject (
    capability_id UUID NOT NULL REFERENCES cerebro_capability(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    subject_type TEXT NOT NULL,
    subject_id UUID NOT NULL,
    relation TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (capability_id, subject_type, subject_id, relation),
    CONSTRAINT cerebro_capability_subject_type_known CHECK (
        subject_type IN ('member', 'agent', 'group', 'runtime', 'workspace')
    ),
    CONSTRAINT cerebro_capability_subject_relation_known CHECK (
        relation IN ('reporter', 'owner', 'user')
    )
);

CREATE INDEX IF NOT EXISTS idx_cerebro_capability_subject_lookup
    ON cerebro_capability_subject (workspace_id, subject_type, subject_id);
