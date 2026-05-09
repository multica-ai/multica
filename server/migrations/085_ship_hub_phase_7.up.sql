-- Ship Hub Phase 7a — the Release object: a coordinated batch of PRs
-- shepherded together through staging → production. Phase 7a is read +
-- create only (no merge train, no promotion automation); the schema
-- ships in full so subsequent phases (7b/7c/7d) just flip stages and
-- bump timestamps without further migrations.
--
-- Modeling decisions:
--
--  * `release_stage` is an enum even though there are 9 values, because
--    every UI surface maps it 1:1 to a column / pill / icon and a typo
--    must be a CREATE TYPE migration, not a free-text drift.
--
--  * The PR membership is a join table with a `is_active` flag. We
--    can't reference `ship_release.stage` from a partial unique index'
--    WHERE clause (that would require a subquery, which Postgres
--    forbids on indexes), so we denormalize "active membership" onto
--    the join row and let the service layer flip it on terminal
--    transitions. The partial unique index on `(pull_request_id) WHERE
--    is_active = TRUE` prevents any PR from ever being in two
--    concurrent releases.
--
--  * `ship_release_event` is append-only. We emit a row per stage
--    transition, per PR add / remove, per metadata edit, per channel /
--    issue auto-create. The detail page reads this back as the timeline
--    panel.

CREATE TYPE release_stage AS ENUM (
    'assembling',     -- Curating PRs; haven't merged yet (Phase 7a entry)
    'merging',        -- Merge train in flight (Phase 7b)
    'in_staging',     -- All merged; staging deploy underway/done (Phase 7c)
    'verifying',      -- Smoke green; awaiting human QA + approver (Phase 7c)
    'promoting',      -- Production deploy in flight (Phase 7d)
    'in_production',  -- Live; in 24h post-deploy monitoring window (Phase 7d)
    'done',           -- Past monitoring window, no rollbacks
    'rolled_back',    -- Production rolled back; release dead (Phase 7d)
    'cancelled'       -- Aborted before merging
);

CREATE TABLE ship_release (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id         UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    project_id           UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    title                TEXT NOT NULL,
    description          TEXT,
    stage                release_stage NOT NULL DEFAULT 'assembling',
    -- Computed at create time as the max() of every member PR's risk_level.
    -- Denormalized so a release-list query doesn't need to re-aggregate
    -- across the join table on every render.
    risk_level           risk_level NOT NULL DEFAULT 'medium',
    -- Auto-created channel + issue (see service layer). Both are
    -- ON DELETE SET NULL: hard-deleting either side leaves the release
    -- intact but unlinked, which the UI renders as "channel/issue
    -- removed" rather than blowing up.
    channel_id           UUID REFERENCES channel(id) ON DELETE SET NULL,
    issue_id             UUID REFERENCES issue(id) ON DELETE SET NULL,
    -- Approver chain. `second_approver_id` is required at promote time
    -- when risk_level = 'critical'; Phase 7a stores the column but
    -- doesn't enforce yet (preflight gate covers that).
    approver_id          UUID REFERENCES "user"(id) ON DELETE SET NULL,
    second_approver_id   UUID REFERENCES "user"(id) ON DELETE SET NULL,
    -- Phase 7c/7d link to the deploys this release produced. Stored
    -- here so the detail page can render "this release shipped as
    -- staging deploy <id>" without a roundtrip across deploys.
    staging_deploy_id    UUID REFERENCES deploy(id) ON DELETE SET NULL,
    production_deploy_id UUID REFERENCES deploy(id) ON DELETE SET NULL,
    created_by           UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Timestamp ladder. Each transition stamps the next column; NULL
    -- means "not yet reached this stage". Cheaper than a separate
    -- audit join for the common "when did this release ship?" read.
    merged_at            TIMESTAMPTZ,
    staged_at            TIMESTAMPTZ,
    promoted_at          TIMESTAMPTZ,
    done_at              TIMESTAMPTZ,
    rollback_reason      TEXT
);

-- Workspace-wide "Active releases" rail — the home page widget.
CREATE INDEX idx_ship_release_workspace_stage
    ON ship_release(workspace_id, stage, updated_at DESC);

-- Per-project list (release detail page surfaces siblings).
CREATE INDEX idx_ship_release_project_stage
    ON ship_release(project_id, stage, updated_at DESC);

-- Many-to-one with PRs. position lets the user re-order the merge
-- train (Phase 7b dispatches PRs in this order); for 7a we just
-- preserve insertion order.
CREATE TABLE ship_release_pull_request (
    release_id      UUID NOT NULL REFERENCES ship_release(id) ON DELETE CASCADE,
    pull_request_id UUID NOT NULL REFERENCES pull_request(id) ON DELETE CASCADE,
    position        INTEGER NOT NULL,
    -- Phase 7b populates these after each PR's merge call returns.
    merged_sha      TEXT,
    merged_at       TIMESTAMPTZ,
    merge_error     TEXT,
    added_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Denormalized "this membership counts for the partial unique
    -- index" flag. Service code flips it FALSE when the parent release
    -- transitions to a terminal stage (done / rolled_back / cancelled).
    -- Cannot reference ship_release.stage from the partial index'
    -- predicate, hence the denormalization.
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    PRIMARY KEY (release_id, pull_request_id)
);

-- "What release is this PR in?" reverse lookup.
CREATE INDEX idx_ship_release_pr_pr
    ON ship_release_pull_request(pull_request_id);

-- A PR can be in at most one ACTIVE release. Partial unique index
-- targets only rows where is_active = TRUE; once a release ends
-- (terminal stage), the service flips is_active=FALSE and the PR
-- becomes available for the next release.
CREATE UNIQUE INDEX idx_ship_release_pr_active_unique
    ON ship_release_pull_request(pull_request_id) WHERE is_active = TRUE;

-- Per-release append-only event log. Powers the timeline panel on the
-- detail page. event_type is a free-text string (not enum) because
-- subsequent phases will add new event types and we'd rather not
-- migrate the type every time. Known values:
--   "created"          — release created
--   "pr_added"         — PR added to assembling release
--   "pr_removed"       — PR removed from assembling release
--   "metadata_updated" — title/description/approver edited
--   "approver_set"     — approver_id changed (subset of metadata_updated;
--                         emitted separately because it's the most
--                         interesting individual edit and the timeline
--                         uses a different icon)
--   "channel_created"  — auto-created discussion channel
--   "issue_created"    — auto-created tracker issue
--   "stage_changed"    — release advanced (Phase 7b/c/d)
--   "cancelled"        — release cancelled
CREATE TABLE ship_release_event (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    release_id    UUID NOT NULL REFERENCES ship_release(id) ON DELETE CASCADE,
    event_type    TEXT NOT NULL,
    actor_user_id UUID REFERENCES "user"(id) ON DELETE SET NULL,
    payload       JSONB,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ship_release_event_release
    ON ship_release_event(release_id, created_at DESC);
