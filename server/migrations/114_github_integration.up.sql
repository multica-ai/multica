-- GitHub Projects v2 integration: bidirectional sync between a Multica
-- workspace board and a GitHub Projects v2 board (e.g. katalon-studio
-- Project #16 "Katalon 2.0 Flow").
--
-- Scope notes:
--   * One workspace binds to at most one GitHub project board
--     (github_installation, UNIQUE workspace_id).
--   * github_issue_binding is the item ↔ issue mapping — the GitHub
--     ProjectV2Item node id maps to a Multica issue. It carries enough
--     remote identity (content node id, repo, issue number) to push
--     field/comment changes back without re-querying the board.
--   * Loop-guard columns (last_pushed_status, remote_status) let the
--     outbound patcher skip echoing a change that originated from the
--     inbound importer, and vice-versa.
--   * access_token_encrypted mirrors the lark_installation secretbox
--     pattern. It is nullable: the default local/self-host mode reads a
--     personal access token from MULTICA_GITHUB_TOKEN instead of the DB.
--   * field_map caches the resolved ProjectV2 field + single-select
--     option node ids (Status / Priority / Area / Pod / Intent Ref /
--     Target date) so the outbound patcher does not re-resolve them on
--     every push.

-- =====================
-- github_installation
-- =====================
CREATE TABLE github_installation (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id           UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    org_login              TEXT NOT NULL,
    project_number         INT  NOT NULL,
    project_node_id        TEXT NOT NULL,
    -- Ciphertext of a GitHub token (secretbox). NULL when the operator
    -- supplies the token via MULTICA_GITHUB_TOKEN instead.
    access_token_encrypted BYTEA,
    status                 TEXT NOT NULL DEFAULT 'active'
                           CHECK (status IN ('active', 'paused', 'error')),
    -- Resolved ProjectV2 field ids + single-select option maps, cached
    -- from the board schema so the patcher does not re-resolve per push.
    field_map              JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_synced_at         TIMESTAMPTZ,
    last_error             TEXT,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id),
    UNIQUE (org_login, project_number)
);

-- =====================
-- github_issue_binding
-- =====================
CREATE TABLE github_issue_binding (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    installation_id     UUID NOT NULL REFERENCES github_installation(id) ON DELETE CASCADE,
    workspace_id        UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    multica_issue_id    UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    -- ProjectV2Item node id (the board row).
    github_item_id      TEXT NOT NULL,
    -- Issue / DraftIssue content node id (for comments + field reads).
    github_content_id   TEXT,
    -- "owner/name" of the backing repo issue (NULL for draft items).
    github_repo         TEXT,
    github_issue_number INT,
    -- Loop-guard: last Status string we observed on / pushed to GitHub.
    remote_status       TEXT,
    last_pushed_status  TEXT,
    -- Hash of the last-synced remote field snapshot; lets the importer
    -- skip issues whose remote side has not changed since last poll.
    remote_hash         TEXT,
    synced_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (installation_id, multica_issue_id),
    UNIQUE (installation_id, github_item_id)
);

CREATE INDEX idx_github_issue_binding_item  ON github_issue_binding (github_item_id);
CREATE INDEX idx_github_issue_binding_issue ON github_issue_binding (multica_issue_id);
