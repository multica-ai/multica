-- Durable Memory Docket (H1 hard gate) and its memory items.
-- Docket is one row per subject scope; items carry state, dedupe key, and
-- TTL. No inline indexes.
CREATE TABLE memoryhub_memory_docket (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    scope_kind TEXT NOT NULL CHECK (scope_kind IN ('workspace', 'project')),
    scope_id UUID,
    subject_type TEXT NOT NULL CHECK (subject_type IN ('project', 'agent', 'issue')),
    subject_id UUID NOT NULL,
    policy TEXT NOT NULL DEFAULT 'required' CHECK (policy IN ('required', 'optional')),
    revision INTEGER NOT NULL DEFAULT 1,
    generated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ck_mh_docket_scope CHECK (
        (scope_kind = 'workspace' AND scope_id IS NULL)
        OR (scope_kind = 'project' AND scope_id IS NOT NULL)
    )
);

CREATE TABLE memoryhub_memory_item (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    docket_id UUID NOT NULL,
    state TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'withdrawn', 'expired', 'superseded', 'purged')),
    kind TEXT NOT NULL,
    summary TEXT NOT NULL,
    source_ref TEXT NOT NULL,
    evidence_ref UUID,
    priority INTEGER NOT NULL DEFAULT 0,
    dedupe_key TEXT NOT NULL,
    expires_at TIMESTAMPTZ,
    withdrawn_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
