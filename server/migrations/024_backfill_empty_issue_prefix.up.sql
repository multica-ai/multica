-- Backfill workspaces that have an empty issue_prefix (e.g. auto-created
-- during first login before the prefix was wired up in ensureUserWorkspace).
UPDATE workspace SET issue_prefix = UPPER(
    LEFT(REGEXP_REPLACE(name, '[^a-zA-Z]', '', 'g'), 3)
) WHERE issue_prefix = '';

-- Fallback for workspaces whose name has no alphabetic characters.
UPDATE workspace SET issue_prefix = 'WS' WHERE issue_prefix = '';
