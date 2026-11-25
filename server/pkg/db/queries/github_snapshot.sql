-- =====================
-- GitHub API snapshot (MUL-5265, Plan C)
--
-- These queries back the API-snapshot refresh pipeline. The GitHub GraphQL
-- response is the single source of truth; each successful fetch is written as
-- one atomic batch replace (guarded update of the PR row + full replace of the
-- per-check rows) inside a single transaction.
-- =====================

-- name: ListGitHubPRRowsByAddress :many
-- One (installation, owner, repo, number) address can map to several
-- github_pull_request rows — the same installation can be bound to multiple
-- workspaces (#4823/#4855), each mirroring its own row. A single API fetch is
-- applied to every matching row (each guarded by its own head_sha).
SELECT id, workspace_id, head_sha, state
FROM github_pull_request
WHERE installation_id = $1 AND repo_owner = $2 AND repo_name = $3 AND pr_number = $4;

-- name: UpdateGitHubPRSnapshot :execrows
-- Head-SHA anti-stale write (acceptance criterion 1): the snapshot is written
-- only when the row's current head_sha still equals the head the snapshot was
-- fetched for. If the head advanced (a newer push landed while this request was
-- in flight, mirrored by the pull_request webhook), 0 rows are updated and the
-- caller discards the whole response — the per-check replace is skipped too.
UPDATE github_pull_request
SET api_mergeable          = sqlc.narg('api_mergeable'),
    api_merge_state_status = sqlc.narg('api_merge_state_status'),
    checks_rollup_state    = sqlc.narg('checks_rollup_state'),
    snapshot_head_sha      = sqlc.arg('head_sha'),
    snapshot_fetched_at    = sqlc.arg('fetched_at'),
    updated_at             = now()
WHERE id = sqlc.arg('pr_id') AND head_sha = sqlc.arg('head_sha');

-- name: DeleteGitHubPRCheckRuns :exec
-- First half of the atomic per-check replace. Runs inside the same transaction
-- as UpdateGitHubPRSnapshot and the inserts below.
DELETE FROM github_pull_request_check_run WHERE pr_id = $1;

-- name: InsertGitHubPRCheckRun :exec
INSERT INTO github_pull_request_check_run (
    pr_id, head_sha, ordinal, name, status, conclusion, details_url, is_status_context
) VALUES (
    $1, $2, $3, $4, $5, sqlc.narg('conclusion'), sqlc.narg('details_url'), $6
);

-- name: ListStaleUndecidedGitHubPRs :many
-- TTL / safety-net sweep source. Returns distinct addresses of open/draft PRs
-- whose snapshot is missing for the current head (never fetched or head_sha
-- advanced past snapshot_head_sha) or both stale and undecided. A decided
-- snapshot for the current head leaves the periodic refresh set; later webhook,
-- watched-wakeup, or view activity can still refresh it.
-- The caller advances an address cursor after each bounded batch. Rows after
-- the cursor sort first, followed by a wrap to the start, so even perpetually
-- failing addresses cannot pin the same first LIMIT rows forever.
WITH candidates AS (
    SELECT installation_id, repo_owner, repo_name, pr_number
    FROM github_pull_request AS pr
    WHERE state IN ('open', 'draft')
      AND (
          snapshot_fetched_at IS NULL
          OR (head_sha <> '' AND snapshot_head_sha <> head_sha)
          OR snapshot_fetched_at < sqlc.arg('older_than')
      )
      AND (
          snapshot_fetched_at IS NULL
          OR (head_sha <> '' AND snapshot_head_sha <> head_sha)
          OR api_mergeable IS NULL
          OR api_mergeable = 'UNKNOWN'
          OR checks_rollup_state IN ('PENDING', 'EXPECTED')
          OR EXISTS (
              SELECT 1
              FROM github_pull_request_check_run AS cr
              WHERE cr.pr_id = pr.id AND cr.status <> 'completed'
          )
      )
    GROUP BY installation_id, repo_owner, repo_name, pr_number
)
SELECT installation_id, repo_owner, repo_name, pr_number
FROM candidates
ORDER BY (
    ROW(installation_id, repo_owner, repo_name, pr_number) >
    ROW(
        sqlc.arg('after_installation_id')::BIGINT,
        sqlc.arg('after_repo_owner')::TEXT,
        sqlc.arg('after_repo_name')::TEXT,
        sqlc.arg('after_pr_number')::INTEGER
    )
) DESC,
installation_id, repo_owner, repo_name, pr_number
LIMIT sqlc.arg('max_rows');

-- name: ListStaleConflictWatchedGitHubPRs :many
-- Second sweep source: open/draft PRs linked to an issue with an enabled
-- "wake on merge conflict" rule. A decided snapshot leaves the undecided sweep,
-- but a base-branch move can turn it conflicting without any event for this
-- PR, so watched PRs are re-fetched on the TTL regardless. The candidate set
-- comes from the enabled rules, whose count is capped. Like the undecided
-- sweep, an address cursor rotates the bounded batch, so addresses whose
-- fetches keep failing (their snapshot never gets newer) cannot pin it.
WITH candidates AS (
    SELECT pr.installation_id, pr.repo_owner, pr.repo_name, pr.pr_number
    FROM github_pull_request AS pr
    WHERE pr.state IN ('open', 'draft')
      AND (
          pr.snapshot_fetched_at IS NULL
          OR (pr.head_sha <> '' AND pr.snapshot_head_sha <> pr.head_sha)
          OR pr.snapshot_fetched_at < sqlc.arg('older_than')
      )
      AND pr.id IN (
          SELECT ipr.pull_request_id
          FROM issue_wakeup AS w
          JOIN issue_pull_request AS ipr ON ipr.issue_id = w.issue_id
          WHERE w.enabled
            AND w.condition->>'type' = 'pull_request'
            AND w.condition->>'event' = 'conflicts'
      )
    GROUP BY pr.installation_id, pr.repo_owner, pr.repo_name, pr.pr_number
)
SELECT installation_id, repo_owner, repo_name, pr_number
FROM candidates
ORDER BY (
    ROW(installation_id, repo_owner, repo_name, pr_number) >
    ROW(
        sqlc.arg('after_installation_id')::BIGINT,
        sqlc.arg('after_repo_owner')::TEXT,
        sqlc.arg('after_repo_name')::TEXT,
        sqlc.arg('after_pr_number')::INTEGER
    )
) DESC,
installation_id, repo_owner, repo_name, pr_number
LIMIT sqlc.arg('max_rows');

-- name: ListStaleChecksWatchedGitHubPRs :many
-- Third sweep source: open/draft PRs watched by an enabled "checks finished"
-- wakeup (issue or chat). A PR can leave the undecided sweep while a checks
-- wakeup is still waiting (for example, an initial fetch right after
-- synchronize saw no checks yet, or jobs were re-run on the same head), or
-- webhook/chase refreshes may have been dropped during a rate limit or deploy.
-- Sweeping watched PRs on a shorter TTL keeps --until-pr checks responsive
-- without relying on a manual page visit. Rotates an address cursor like the
-- other sweeps.
WITH watched_issue_prs AS (
    SELECT ipr.pull_request_id AS pr_id
    FROM issue_wakeup AS w
    JOIN issue_pull_request AS ipr ON ipr.issue_id = w.issue_id
    WHERE w.enabled
      AND w.condition->>'type' = 'pull_request'
      AND w.condition->>'event' = 'checks_finished'
),
watched_chat_prs AS (
    SELECT pr.id AS pr_id
    FROM chat_wakeup AS cw
    JOIN github_pull_request AS pr
      ON pr.workspace_id = cw.workspace_id
     AND lower(pr.repo_owner) = lower(split_part(cw.condition->>'repo', '/', 1))
     AND lower(pr.repo_name) = lower(split_part(cw.condition->>'repo', '/', 2))
     AND pr.pr_number = CASE
         WHEN COALESCE(cw.condition->>'number', '') ~ '^[0-9]{1,9}$' THEN (cw.condition->>'number')::INTEGER
     END
    WHERE cw.status = 'active'
      AND cw.kind = 'condition'
      AND cw.condition->>'type' = 'pull_request'
      AND cw.condition->>'event' = 'checks_finished'
),
candidates AS (
    SELECT pr.installation_id, pr.repo_owner, pr.repo_name, pr.pr_number
    FROM github_pull_request AS pr
    WHERE pr.state IN ('open', 'draft')
      AND (
          pr.snapshot_fetched_at IS NULL
          OR (pr.head_sha <> '' AND pr.snapshot_head_sha <> pr.head_sha)
          OR pr.snapshot_fetched_at < sqlc.arg('older_than')
      )
      AND (
          pr.id IN (SELECT pr_id FROM watched_issue_prs)
          OR pr.id IN (SELECT pr_id FROM watched_chat_prs)
      )
    GROUP BY pr.installation_id, pr.repo_owner, pr.repo_name, pr.pr_number
)
SELECT installation_id, repo_owner, repo_name, pr_number
FROM candidates
ORDER BY (
    ROW(installation_id, repo_owner, repo_name, pr_number) >
    ROW(
        sqlc.arg('after_installation_id')::BIGINT,
        sqlc.arg('after_repo_owner')::TEXT,
        sqlc.arg('after_repo_name')::TEXT,
        sqlc.arg('after_pr_number')::INTEGER
    )
) DESC,
installation_id, repo_owner, repo_name, pr_number
LIMIT sqlc.arg('max_rows');

-- name: GetGitHubPRForRefresh :one
-- Looks up one mirrored PR by case-insensitive owner/repo and number so a
-- chat wakeup targeting a PR can kick off an immediate snapshot refresh.
SELECT installation_id, repo_owner, repo_name, pr_number, state, head_sha, snapshot_head_sha, snapshot_fetched_at
FROM github_pull_request
WHERE workspace_id = $1
  AND lower(repo_owner) = lower(sqlc.arg('repo_owner')::text)
  AND lower(repo_name) = lower(sqlc.arg('repo_name')::text)
  AND pr_number = $2
ORDER BY updated_at DESC
LIMIT 1;

-- name: ListConflictWatchedGitHubPRNumbersInRepo :many
-- Open/draft PRs in one repository that an enabled "wake on merge conflict"
-- rule watches. A merge into the repository can make them conflicting without
-- an event of their own, so the merge webhook re-fetches them.
SELECT DISTINCT pr.pr_number
FROM issue_wakeup AS w
JOIN issue_pull_request AS ipr ON ipr.issue_id = w.issue_id
JOIN github_pull_request AS pr ON pr.id = ipr.pull_request_id
WHERE w.enabled
  AND w.condition->>'type' = 'pull_request'
  AND w.condition->>'event' = 'conflicts'
  AND pr.installation_id = $1 AND pr.repo_owner = $2 AND pr.repo_name = $3
  AND pr.state IN ('open', 'draft')
  AND pr.pr_number <> $4
ORDER BY pr.pr_number
LIMIT 200;

-- name: ListGitHubPRNumbersByHeadSHA :many
-- Resolves a commit SHA to the PR numbers whose head it is. `status` webhook
-- events (legacy commit statuses) carry a SHA + repo but no PR number, so we
-- map back through the mirrored head_sha to find which PR(s) to refresh.
SELECT DISTINCT pr_number
FROM github_pull_request
WHERE installation_id = $1 AND repo_owner = $2 AND repo_name = $3 AND head_sha = $4;

-- name: GetGitHubPullRequestByID :one
SELECT * FROM github_pull_request WHERE id = $1;
