-- Knowledge taxonomy and policy (spec 3.3, plan 10.6 to 10.10): the kinds a
-- document can have, the publish policy per base or kind, the retrieval eval
-- cases and their runs, and the latest result of each Librarian job.
--
-- No foreign keys (repo rule): workspace, squad and link references are
-- checked in application code. Indexes are built CONCURRENTLY in 752 to 758,
-- one statement per file. New tables carry unique indexes instead of primary
-- key constraints, so each index stays its own migration.

CREATE TABLE knowledge_kind (
    workspace_id UUID NOT NULL,
    key TEXT NOT NULL CHECK (key ~ '^[a-z][a-z0-9_]{1,63}$'),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 64),
    -- The base a document of this kind goes to when the caller names none.
    default_base_key TEXT NOT NULL DEFAULT '',
    -- Markdown skeleton. Every required section must appear in it as a heading.
    template TEXT NOT NULL DEFAULT '',
    -- Freshness: a published document older than this is reviewed. Zero means
    -- the kind does not expire.
    review_days INTEGER NOT NULL DEFAULT 0 CHECK (review_days >= 0),
    required_sections TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- scope = 'base' with a knowledge base key, or 'kind' with a kind key. A kind
-- row wins over the base row for documents of that kind.
CREATE TABLE knowledge_publish_policy (
    workspace_id UUID NOT NULL,
    scope TEXT NOT NULL CHECK (scope IN ('base', 'kind')),
    scope_key TEXT NOT NULL,
    mode TEXT NOT NULL CHECK (mode IN ('manual', 'auto_after_review', 'auto')),
    updated_by UUID,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE knowledge_eval_case (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    question TEXT NOT NULL CHECK (char_length(question) BETWEEN 1 AND 500),
    -- A hit matches when a top result has this kind, or is this WeKnora document.
    expected_kind TEXT NOT NULL DEFAULT '',
    expected_document_id TEXT NOT NULL DEFAULT '',
    owner_squad_id UUID,
    created_by_type TEXT NOT NULL CHECK (created_by_type IN ('agent', 'member', 'system')),
    created_by_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE knowledge_eval_run (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    total INTEGER NOT NULL CHECK (total > 0),
    hits INTEGER NOT NULL CHECK (hits >= 0 AND hits <= total),
    hit_rate DOUBLE PRECISION NOT NULL CHECK (hit_rate >= 0 AND hit_rate <= 1),
    -- One entry per case: case id, question, whether it hit, and the rank of
    -- the first matching result (zero when none).
    results JSONB NOT NULL DEFAULT '[]',
    ran_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The latest summary of each Librarian job (daily triage, freshness, taxonomy
-- audit, eval). The digest and the Librarian read the newest row per job.
CREATE TABLE knowledge_health_run (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    job TEXT NOT NULL,
    summary JSONB NOT NULL DEFAULT '{}',
    ran_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Taxonomy on every document. kind '' is a document written before kinds, and
-- is what the taxonomy audit reports as missing a kind.
ALTER TABLE knowledge_link
    ADD COLUMN kind TEXT NOT NULL DEFAULT '',
    ADD COLUMN initiative_id UUID,
    ADD COLUMN stage_key TEXT NOT NULL DEFAULT '',
    ADD COLUMN squad_id UUID,
    ADD COLUMN confidence TEXT CHECK (confidence IN ('low', 'medium', 'high')),
    ADD COLUMN valid_until DATE,
    ADD COLUMN supersedes_link_id UUID,
    ADD COLUMN tags TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN review_verdict TEXT NOT NULL DEFAULT '' CHECK (review_verdict IN ('', 'ok', 'changes')),
    ADD COLUMN review_by_id UUID,
    ADD COLUMN review_squad_id UUID,
    ADD COLUMN review_note TEXT NOT NULL DEFAULT '',
    ADD COLUMN reviewed_at TIMESTAMPTZ;
