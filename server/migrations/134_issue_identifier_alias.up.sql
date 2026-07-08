-- Issue identifier aliases: when an issue moves to another space it is
-- renumbered (numbers are per-space), so its old identifier (e.g. MUL-3)
-- would stop resolving. Every move records the pre-move identifier here so
-- identifier lookups (API/CLI, GitHub branch/PR auto-linking) can fall back
-- to the alias and land on the moved issue. Numbers are never reused
-- (issue_counter is monotonic), so an alias can never shadow a live issue.
CREATE TABLE issue_identifier_alias (
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    space_key_lower TEXT NOT NULL,
    number INTEGER NOT NULL,
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, space_key_lower, number)
);

CREATE INDEX idx_issue_identifier_alias_issue ON issue_identifier_alias(issue_id);
