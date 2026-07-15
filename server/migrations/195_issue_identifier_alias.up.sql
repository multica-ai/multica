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

-- Backfill aliases for the renaming migration 193 already performed. 193
-- normalized every workspace's legacy issue_prefix into a Space key
-- satisfying ^[A-Z][A-Z0-9]{0,6}$ (uppercase, strip non-alnum, truncate to 7).
-- Legacy prefixes longer than 7 chars, digit-first, or punctuated therefore
-- diverge from the key their issues now resolve under, and — unlike a
-- runtime move-to-space — that rename never went through the alias-writing
-- path above, since no issue actually changed space_id. Old links/CLI/GitHub
-- references using the pre-migration prefix would otherwise 404 forever.
-- workspace.issue_prefix is untouched by 161/162 (kept intentionally; see
-- 162's header comment), so the original value is still available here.
INSERT INTO issue_identifier_alias (workspace_id, space_key_lower, number, issue_id)
SELECT i.workspace_id, lower(btrim(w.issue_prefix)), i.number, i.id
FROM issue i
JOIN workspace w ON w.id = i.workspace_id
JOIN workspace_space wt ON wt.id = i.space_id
WHERE w.issue_prefix IS NOT NULL
  AND btrim(w.issue_prefix) <> ''
  AND lower(btrim(w.issue_prefix)) <> lower(wt.key)
ON CONFLICT DO NOTHING;
