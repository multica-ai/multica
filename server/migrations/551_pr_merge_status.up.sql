-- PR merge automation target (MUL-7726). The workspace setting
-- settings.pr_merge_status picks what merging every PR linked to an issue does:
-- "none", or the key of the status the issue moves to. Absent means Done.
--
-- Until now a merge completed an issue only when a PR said "Closes MUL-1" in
-- its title or body, and settings.pr_auto_complete_enabled = false turned that
-- off. Pin "none" on existing workspaces whose history says a merge never
-- completed their issues regardless of the text, so the new Done default only
-- reaches workspaces that never linked a PR or only ever merged keyword PRs:
--
--   * pr_auto_complete_enabled = false                         -> none
--   * a linked PR exists, and not every merged link carried a
--     closing keyword (or nothing merged yet)                  -> none
--   * no linked PR, or every merged link carried a keyword     -> left absent
--
-- Pinned workspaces also get pr_auto_complete_enabled = false, the switch
-- desktop clients from before this change still show, so they show it off.
-- Only workspaces without an explicit pr_merge_status are touched, so a replay
-- is harmless. The scan reads link rows once; the write touches only the
-- pinned workspaces.
WITH link_history AS (
    SELECT i.workspace_id,
           bool_or(pr.state = 'merged') AS any_merged,
           bool_and(pr.state <> 'merged' OR ipr.close_intent) AS merged_with_keyword
    FROM issue_pull_request ipr
    JOIN issue i ON i.id = ipr.issue_id
    JOIN github_pull_request pr ON pr.id = ipr.pull_request_id
    GROUP BY i.workspace_id
    UNION ALL
    SELECT i.workspace_id,
           bool_or(pr.state = 'merged') AS any_merged,
           bool_and(pr.state <> 'merged' OR ipr.close_intent) AS merged_with_keyword
    FROM issue_vcs_pull_request ipr
    JOIN issue i ON i.id = ipr.issue_id
    JOIN vcs_pull_request pr ON pr.id = ipr.pull_request_id
    GROUP BY i.workspace_id
),
pinned AS (
    SELECT workspace_id
    FROM link_history
    GROUP BY workspace_id
    HAVING NOT (bool_or(any_merged) AND bool_and(merged_with_keyword))
)
UPDATE workspace AS w
SET settings = (CASE WHEN jsonb_typeof(w.settings) = 'object' THEN w.settings ELSE '{}'::jsonb END)
    || '{"pr_merge_status": "none", "pr_auto_complete_enabled": false}'::jsonb
WHERE NOT (jsonb_typeof(w.settings) = 'object' AND w.settings ? 'pr_merge_status')
  AND (
      (jsonb_typeof(w.settings) = 'object' AND w.settings->>'pr_auto_complete_enabled' = 'false')
      OR w.id IN (SELECT workspace_id FROM pinned)
  );
