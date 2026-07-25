-- MUL-5265: GitHub API snapshot for PR cards (Plan C).
--
-- The PR card's CI status and mergeability are now sourced from an
-- authenticated GitHub API snapshot (GraphQL pullRequest query) rather than
-- inferred from webhook check_suite events. Webhooks and page visits only
-- trigger a refresh; the API response is the single source of truth and is
-- written as one atomic batch replace per PR.
--
-- All new columns are nullable / defaulted so rows that pre-date the snapshot
-- (or deployments without a GitHub App private key, where the feature degrades
-- off) keep working: the card simply hides the CI / merge region until a
-- snapshot lands.

ALTER TABLE github_pull_request
    -- GraphQL `mergeable`: MERGEABLE / CONFLICTING / UNKNOWN. Answers only
    -- "is there a merge conflict". NULL until first snapshot.
    ADD COLUMN api_mergeable TEXT,
    -- GraphQL `mergeStateStatus`: CLEAN / DIRTY / BLOCKED / BEHIND / UNSTABLE /
    -- DRAFT / HAS_HOOKS / UNKNOWN. "Ready to merge" is derived ONLY from CLEAN.
    ADD COLUMN api_merge_state_status TEXT,
    -- GraphQL statusCheckRollup.state: SUCCESS / FAILURE / PENDING / ERROR /
    -- EXPECTED. NULL means statusCheckRollup was null → "no checks yet", which
    -- must never be rendered as passed.
    ADD COLUMN checks_rollup_state TEXT,
    -- The head SHA the snapshot was fetched for. Pinned so a slow response for
    -- an old head cannot overwrite a newer head's snapshot (head-SHA anti-stale
    -- write). Empty until first snapshot.
    ADD COLUMN snapshot_head_sha TEXT NOT NULL DEFAULT '',
    -- When the snapshot was fetched. Drives the TTL / page-visit refresh and the
    -- stale visual marker. NULL until first snapshot.
    ADD COLUMN snapshot_fetched_at TIMESTAMPTZ;

-- Per-check snapshot rows for a PR's current head. Replaced atomically (delete
-- all + insert) on every successful API fetch — no incremental inference. Both
-- GraphQL CheckRun and StatusContext contexts are normalized into this shape at
-- write time (see ghsnapshot.normalizeContext). Rows are addressed by
-- (pr_id, ordinal): two checks can share a name (matrix jobs, re-runs), so name
-- is not unique. The (pr_id, ordinal) UNIQUE index is created CONCURRENTLY in
-- the next migration (223) — no index (including a PRIMARY KEY's) may be built
-- non-concurrently in a migration, even on a new table (see CLAUDE.md), so the
-- table is created without a primary key and the unique index is added in its
-- own single-statement migration.
CREATE TABLE github_pull_request_check_run (
    pr_id             UUID    NOT NULL,
    head_sha          TEXT    NOT NULL,
    ordinal           INTEGER NOT NULL,
    name              TEXT    NOT NULL,
    -- Normalized lifecycle: 'queued' / 'in_progress' / 'completed'.
    status            TEXT    NOT NULL,
    -- Normalized conclusion: 'success' / 'failure' / 'neutral' / 'cancelled' /
    -- 'skipped' / 'timed_out' / 'action_required' / 'error' / ... ; NULL while
    -- the check is still running.
    conclusion        TEXT,
    details_url       TEXT,
    -- TRUE for legacy commit-status contexts (GraphQL StatusContext), FALSE for
    -- Checks API runs (GraphQL CheckRun). Kept for display / debugging.
    is_status_context BOOLEAN NOT NULL DEFAULT FALSE
);
