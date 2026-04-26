-- 101_issue_label_polish: wire up the existing issue_label / issue_to_label
-- tables with uniqueness, audit columns, and a reverse-lookup index.
-- These tables have existed since 001_init but were never used. This migration
-- adds everything needed to ship an end-to-end "tags" feature.

-- Case-insensitive uniqueness of tag name per workspace. Prevents duplicates
-- like "Bug" and "bug" in the same workspace.
CREATE UNIQUE INDEX IF NOT EXISTS uq_issue_label_workspace_name_ci
    ON issue_label (workspace_id, LOWER(name));

-- Reverse lookup: "list all issues with this label" (used by filter + delete
-- confirmation count).
CREATE INDEX IF NOT EXISTS idx_issue_to_label_label
    ON issue_to_label (label_id);

-- Audit columns on the label itself. Nullable creator_id for historical
-- compatibility; new labels are required to have one (enforced in service).
ALTER TABLE issue_label
    ADD COLUMN IF NOT EXISTS created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN IF NOT EXISTS updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN IF NOT EXISTS creator_type TEXT        NOT NULL DEFAULT 'member'
        CHECK (creator_type IN ('member', 'agent')),
    ADD COLUMN IF NOT EXISTS creator_id   UUID;

-- Constrain color to a small, whitelisted palette. 10 colors chosen to align
-- with the existing Multica design tokens.
ALTER TABLE issue_label
    DROP CONSTRAINT IF EXISTS issue_label_color_check;

ALTER TABLE issue_label
    ADD CONSTRAINT issue_label_color_check
        CHECK (color IN (
            'slate', 'gray', 'red', 'orange', 'amber',
            'green', 'teal', 'blue', 'indigo', 'purple', 'pink'
        ));

-- Assignment audit: when a label was attached to an issue, and by whom.
-- Useful for activity log rendering and for debugging agent tag behaviour.
ALTER TABLE issue_to_label
    ADD COLUMN IF NOT EXISTS assigned_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN IF NOT EXISTS actor_type  TEXT        NOT NULL DEFAULT 'member'
        CHECK (actor_type IN ('member', 'agent', 'system')),
    ADD COLUMN IF NOT EXISTS actor_id    UUID;
