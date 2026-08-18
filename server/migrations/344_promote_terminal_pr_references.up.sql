-- Repair historical links that were created from a bare PR body mention and
-- therefore hidden as reference_only, then later gained a routable issue key
-- in the mirrored title or branch after the PR was already terminal.
--
-- The webhook upsert now performs the same one-way promotion for future
-- deliveries. This migration makes the rollout repair existing rows without a
-- GitHub replay and is idempotent because it only changes TRUE to FALSE.
UPDATE issue_pull_request AS ipr
SET reference_only = FALSE
FROM issue AS i, workspace AS w, github_pull_request AS pr
WHERE ipr.reference_only
  AND ipr.issue_id = i.id
  AND w.id = i.workspace_id
  AND pr.id = ipr.pull_request_id
  AND pr.workspace_id = i.workspace_id
  AND (
      lower(pr.title) ~ (
          '(^|[^a-z0-9])' || lower(w.issue_prefix || '-' || i.number::text) ||
          '([^a-z0-9]|$)'
      )
      OR lower(COALESCE(pr.branch, '')) ~ (
          '(^|[^a-z0-9])' || lower(w.issue_prefix || '-' || i.number::text) ||
          '([^a-z0-9]|$)'
      )
  );
