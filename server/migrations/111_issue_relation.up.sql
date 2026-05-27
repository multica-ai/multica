-- First-class issue relations / backlinks.
--
-- ITT-237 Phase 1 foundation. Models a directed reference from one issue
-- (source) to another (target). Backlinks are simply the reverse-direction
-- query (rows where target_issue_id = X).
--
-- This is a pure cross-reference relation: creating a row here is a data
-- link only. It MUST NOT notify members or trigger agents — those side
-- effects remain exclusive to member/agent/squad @mentions. Nothing in this
-- migration wires relations into any notification or task-queue path.
--
-- Note: the legacy `issue_dependency` table (from 001_init) models a
-- different concept (blocking dependencies) and is currently unused — no
-- sqlc query references it. It is intentionally left untouched here to keep
-- this migration low-risk; dropping it is a separate cleanup.
CREATE TABLE issue_relation (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    source_issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    target_issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    relation_type TEXT NOT NULL DEFAULT 'relates_to'
        CHECK (relation_type IN ('relates_to', 'references')),
    created_by_type TEXT NOT NULL CHECK (created_by_type IN ('member', 'agent', 'system')),
    created_by_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- A directed relation of a given type between two issues is unique, so
    -- re-saving the same reference is idempotent.
    UNIQUE (source_issue_id, target_issue_id, relation_type),
    -- An issue cannot relate to itself.
    CONSTRAINT issue_relation_no_self CHECK (source_issue_id <> target_issue_id)
);

-- Forward lookups: "issues this issue references".
CREATE INDEX idx_issue_relation_source ON issue_relation(source_issue_id);
-- Backlink lookups: "issues that reference this issue".
CREATE INDEX idx_issue_relation_target ON issue_relation(target_issue_id);
-- Workspace-scoped maintenance / listing.
CREATE INDEX idx_issue_relation_workspace ON issue_relation(workspace_id);
