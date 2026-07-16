-- Custom issue statuses (MUL-4809), Phase 1: rollback-safe schema.
--
-- Per-workspace catalog of issue statuses. Each status belongs to exactly one
-- of 5 immutable Categories (backlog | todo | in_progress | done | cancelled),
-- which is the ONLY machine-readable semantics; name / icon / color /
-- description are human-facing only. Seven built-in statuses carry a stable
-- system_key and are seeded per workspace by internal/issuestatus.Ensure;
-- custom statuses have system_key = NULL.
--
-- No foreign keys / cascades (CLAUDE.md): workspace_id is an application-level
-- reference; tenant consistency and cleanup are enforced in app code (the
-- workspace-delete path removes these rows in the same transaction). All
-- indexes are created CONCURRENTLY in their own single-statement follow-up
-- migrations (203-206) because Postgres forbids CREATE INDEX CONCURRENTLY
-- inside this table-create transaction.
CREATE TABLE issue_status (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    icon TEXT NOT NULL,
    color TEXT NOT NULL,
    category TEXT NOT NULL CHECK (
        category IN ('backlog', 'todo', 'in_progress', 'done', 'cancelled')
    ),
    -- Built-in statuses have a stable system_key; custom statuses are NULL.
    -- category and system_key are immutable after creation (enforced in app).
    system_key TEXT CHECK (
        system_key IS NULL OR system_key IN (
            'backlog', 'todo', 'in_progress', 'in_review',
            'blocked', 'done', 'cancelled'
        )
    ),
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    position DOUBLE PRECISION NOT NULL DEFAULT 0,
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
