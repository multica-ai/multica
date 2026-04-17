-- Phase 2a: cache schema additions for the GitLab integration.
-- Purely additive — no DROPs. Existing functionality continues to work.
-- Phase 5 cleanup will drop the now-unused legacy columns/tables once
-- every issue has migrated to the cache shape.

-- Cache-ref columns on issue. Nullable for now: legacy direct-to-DB writes
-- (still allowed for non-connected workspaces) won't supply them.
ALTER TABLE issue
    ADD COLUMN gitlab_iid INT,
    ADD COLUMN gitlab_project_id BIGINT,
    ADD COLUMN external_updated_at TIMESTAMPTZ;

-- The sync writes synthetic rows whose Multica-side creator we don't yet
-- have a mapping for (Phase 3 introduces user_gitlab_connection lookup).
-- Relax the NOT NULL so synced rows can leave creator_id as NULL.
ALTER TABLE issue ALTER COLUMN creator_id DROP NOT NULL;
ALTER TABLE issue ALTER COLUMN creator_type DROP NOT NULL;

-- Unique cache key, scoped per workspace.
CREATE UNIQUE INDEX idx_issue_gitlab_iid
    ON issue(workspace_id, gitlab_iid)
    WHERE gitlab_iid IS NOT NULL;

-- Cache-ref columns on comment.
ALTER TABLE comment
    ADD COLUMN gitlab_note_id BIGINT,
    ADD COLUMN external_updated_at TIMESTAMPTZ;

CREATE UNIQUE INDEX idx_comment_gitlab_note
    ON comment(gitlab_note_id)
    WHERE gitlab_note_id IS NOT NULL;

-- Cache-ref columns on issue_reaction.
ALTER TABLE issue_reaction
    ADD COLUMN gitlab_award_id BIGINT,
    ADD COLUMN external_updated_at TIMESTAMPTZ;

CREATE UNIQUE INDEX idx_issue_reaction_gitlab_award
    ON issue_reaction(gitlab_award_id)
    WHERE gitlab_award_id IS NOT NULL;

-- Cache-ref column on attachment.
ALTER TABLE attachment
    ADD COLUMN gitlab_upload_url TEXT;

-- Label cache (one row per GitLab label per workspace).
CREATE TABLE IF NOT EXISTS gitlab_label (
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    gitlab_label_id BIGINT NOT NULL,
    name TEXT NOT NULL,
    color TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    external_updated_at TIMESTAMPTZ,
    PRIMARY KEY (workspace_id, gitlab_label_id)
);

CREATE INDEX idx_gitlab_label_name ON gitlab_label(workspace_id, name);

-- Issue ↔ GitLab label association. Named issue_gitlab_label to avoid
-- collision with the existing issue_to_label (Multica-native labels) table.
-- Phase 5 will consolidate once the legacy label system is retired.
CREATE TABLE IF NOT EXISTS issue_gitlab_label (
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL,
    gitlab_label_id BIGINT NOT NULL,
    PRIMARY KEY (issue_id, gitlab_label_id),
    FOREIGN KEY (workspace_id, gitlab_label_id)
        REFERENCES gitlab_label(workspace_id, gitlab_label_id) ON DELETE CASCADE
);

CREATE INDEX idx_issue_gitlab_label_label
    ON issue_gitlab_label(workspace_id, gitlab_label_id);

-- Cache of GitLab project members for assignee picker / avatar rendering.
CREATE TABLE IF NOT EXISTS gitlab_project_member (
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    gitlab_user_id BIGINT NOT NULL,
    username TEXT NOT NULL,
    name TEXT NOT NULL,
    avatar_url TEXT NOT NULL DEFAULT '',
    external_updated_at TIMESTAMPTZ,
    PRIMARY KEY (workspace_id, gitlab_user_id)
);

-- Server-only ordering (replaces issue.position once Phase 5 drops it).
-- Rows written on drag-reorder mutations; absent rows fall back to
-- created_at DESC ordering at read time.
CREATE TABLE IF NOT EXISTS issue_position (
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    gitlab_iid INT NOT NULL,
    position NUMERIC NOT NULL,
    PRIMARY KEY (workspace_id, gitlab_iid)
);
