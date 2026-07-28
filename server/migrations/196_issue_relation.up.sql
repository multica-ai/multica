-- Structured issue relations (blocks / related).
--
-- Directed junction between two issues in the same workspace. One canonical row
-- per edge; the inverse view is derived by the reader, never stored:
--   * 'blocks'  — directed: source_issue_id blocks target_issue_id. "Blocked by"
--                 is derived by reading the rows where the issue is the target.
--   * 'related' — symmetric: canonicalized to source_issue_id < target_issue_id
--                 (uuid order) so A~B and B~A collapse to one row. The CHECK
--                 below makes that a hard invariant, not just a handler
--                 convention.
--
-- No foreign keys or cascades (repo rule): both issues are validated in the
-- handler, and rows are cleaned up in the same transaction as issue deletion
-- (see deleteIssueWithRelations) and workspace deletion (see DeleteWorkspace).
-- workspace_id is denormalized so every query filters by workspace without a
-- join (both endpoints always belong to the same workspace).
--
-- The dormant, unused issue_dependency table (shipped empty in 001, never wired
-- up) is intentionally left in place and superseded by this table; a dedicated
-- follow-up can drop it after verifying no deployment holds rows.
--
-- Uniqueness and the reverse-lookup index are built concurrently in the
-- follow-up single-statement migrations 197 and 198.
CREATE TABLE issue_relation (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    source_issue_id UUID NOT NULL,
    target_issue_id UUID NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('blocks', 'related')),
    created_by_type TEXT CHECK (created_by_type IN ('member', 'agent')),
    created_by_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- An issue can never relate to itself (the handler also rejects this with a
    -- 400 before reaching the database).
    CONSTRAINT issue_relation_no_self CHECK (source_issue_id <> target_issue_id),
    -- Symmetric 'related' edges are stored in a single canonical orientation.
    -- uuid comparison matches the handler's canonicalization (uuidToString), so
    -- an un-canonicalized related insert fails loudly instead of duplicating.
    CONSTRAINT issue_relation_related_canonical
        CHECK (type <> 'related' OR source_issue_id < target_issue_id),
    -- Creator attribution is all-or-nothing (both columns set, or both null).
    CONSTRAINT issue_relation_creator_paired
        CHECK ((created_by_type IS NULL) = (created_by_id IS NULL))
);
