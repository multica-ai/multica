-- Plan Overview relational model.
--
-- Locks: CREATE TABLE locks only new empty relations; no existing table is
-- referenced, scanned, or rewritten. Expected duration at production scale:
-- <1 second excluding lock-queue time.
--
-- Reversibility: schema-reversible by 467_project_plan.down.sql. The down
-- migration destroys plan data, so it is data-safe only before plan writes or
-- after an explicit export. No existing project or issue data is modified.

CREATE TABLE project_plan_kind (
    key TEXT NOT NULL CHECK (key ~ '^[a-z][a-z0-9_]*$'),
    display_name TEXT NOT NULL CHECK (char_length(display_name) >= 1),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO project_plan_kind (key, display_name)
VALUES
    ('prd', 'PRD'),
    ('spec', 'Spec'),
    ('sprint', 'Sprint');

CREATE TABLE project_plan (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    project_id UUID NOT NULL,
    version INTEGER NOT NULL CHECK (version >= 1),
    kind TEXT NOT NULL,
    origin TEXT NOT NULL CHECK (origin IN ('manual', 'issue')),
    title TEXT NOT NULL CHECK (char_length(title) >= 1),
    description TEXT NOT NULL DEFAULT '',
    attributes JSONB CHECK (
        attributes IS NULL OR jsonb_typeof(attributes) = 'object'
    ),
    source_issue_id UUID,
    source_issue_revision BIGINT,
    source_description_snapshot TEXT,
    source_content_sha256 TEXT,
    created_by_type TEXT NOT NULL CHECK (created_by_type IN ('member', 'agent')),
    created_by_id UUID NOT NULL,
    superseded_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT project_plan_source_provenance_check CHECK (
        (
            origin = 'manual'
            AND source_issue_id IS NULL
            AND source_issue_revision IS NULL
            AND source_description_snapshot IS NULL
            AND source_content_sha256 IS NULL
        )
        OR
        (
            origin = 'issue'
            AND source_issue_revision IS NOT NULL
            AND source_issue_revision >= 1
            AND source_description_snapshot IS NOT NULL
            AND source_content_sha256 IS NOT NULL
            AND source_content_sha256 ~ '^[0-9a-f]{64}$'
        )
    )
);

CREATE TABLE project_plan_phase (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    project_plan_id UUID NOT NULL,
    title TEXT NOT NULL CHECK (char_length(title) >= 1),
    description TEXT NOT NULL DEFAULT '',
    attributes JSONB CHECK (
        attributes IS NULL OR jsonb_typeof(attributes) = 'object'
    ),
    position INTEGER NOT NULL CHECK (position >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE project_plan_part (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    project_plan_id UUID NOT NULL,
    project_plan_phase_id UUID NOT NULL,
    title TEXT NOT NULL CHECK (char_length(title) >= 1),
    description TEXT NOT NULL DEFAULT '',
    acceptance_criteria TEXT NOT NULL DEFAULT '',
    attributes JSONB CHECK (
        attributes IS NULL OR jsonb_typeof(attributes) = 'object'
    ),
    position INTEGER NOT NULL CHECK (position >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE project_plan_part_issue (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    project_plan_id UUID NOT NULL,
    project_plan_part_id UUID NOT NULL,
    issue_id UUID,
    issue_number_snapshot INTEGER NOT NULL CHECK (issue_number_snapshot >= 1),
    issue_title_snapshot TEXT NOT NULL CHECK (char_length(issue_title_snapshot) >= 1),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE project_plan_dependency (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    project_plan_id UUID NOT NULL,
    blocked_phase_id UUID,
    blocked_part_id UUID,
    blocking_phase_id UUID,
    blocking_part_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT project_plan_dependency_blocked_node_check CHECK (
        num_nonnulls(blocked_phase_id, blocked_part_id) = 1
    ),
    CONSTRAINT project_plan_dependency_blocking_node_check CHECK (
        num_nonnulls(blocking_phase_id, blocking_part_id) = 1
    ),
    CONSTRAINT project_plan_dependency_not_self_check CHECK (
        blocked_phase_id IS DISTINCT FROM blocking_phase_id
        OR blocked_phase_id IS NULL
        OR blocking_phase_id IS NULL
    ),
    CONSTRAINT project_plan_dependency_part_not_self_check CHECK (
        blocked_part_id IS DISTINCT FROM blocking_part_id
        OR blocked_part_id IS NULL
        OR blocking_part_id IS NULL
    )
);
