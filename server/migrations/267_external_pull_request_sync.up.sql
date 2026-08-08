ALTER TABLE external_pull_request
    ADD COLUMN state TEXT NOT NULL DEFAULT 'unknown' CHECK (state IN ('open', 'closed', 'merged', 'draft', 'unknown')),
    ADD COLUMN source_branch TEXT,
    ADD COLUMN target_branch TEXT,
    ADD COLUMN author_login TEXT,
    ADD COLUMN pr_created_at TIMESTAMPTZ,
    ADD COLUMN pr_updated_at TIMESTAMPTZ,
    ADD COLUMN ready_to_merge BOOLEAN,
    ADD COLUMN comment_count INTEGER NOT NULL DEFAULT 0 CHECK (comment_count >= 0),
    ADD COLUMN unresolved_comment_count INTEGER NOT NULL DEFAULT 0 CHECK (unresolved_comment_count >= 0),
    ADD COLUMN sync_requested_at TIMESTAMPTZ,
    ADD COLUMN last_sync_at TIMESTAMPTZ,
    ADD COLUMN sync_error TEXT;
