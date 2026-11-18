-- A missing row is unenrolled, never evidence that remote deletion completed.
-- Keep deleted rows as tombstones; only an explicit desired-state request may
-- restore one. Relationships are validated by callers, without foreign keys.
CREATE TABLE kagent_enrollment (
    agent_id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    runtime_id UUID NOT NULL,
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    generation BIGINT NOT NULL DEFAULT 1 CHECK (generation > 0),
    desired_state TEXT NOT NULL DEFAULT 'present'
        CHECK (desired_state IN ('present', 'absent')),
    phase TEXT NOT NULL DEFAULT 'pending'
        CHECK (phase IN ('pending', 'ready', 'draining', 'deleting', 'deleted', 'blocked')),
    applied_generation BIGINT,
    -- Only sanitized, non-sensitive diagnostic text may be written here.
    last_error TEXT NOT NULL DEFAULT '' CHECK (length(last_error) <= 2048),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (applied_generation IS NULL OR
        (applied_generation > 0 AND applied_generation <= generation)),
    CHECK ((desired_state = 'present' AND phase IN ('pending', 'ready', 'blocked')) OR
        (desired_state = 'absent' AND phase IN ('draining', 'deleting', 'deleted', 'blocked'))),
    CHECK (phase NOT IN ('ready', 'deleted') OR
        (applied_generation IS NOT NULL AND applied_generation = generation))
);
