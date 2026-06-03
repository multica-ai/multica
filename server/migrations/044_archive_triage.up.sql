ALTER TABLE issue
    ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ DEFAULT NULL;

ALTER TABLE issue
    ADD COLUMN IF NOT EXISTS archived_by UUID DEFAULT NULL REFERENCES "user"(id);

CREATE INDEX IF NOT EXISTS idx_issue_workspace_archived ON issue(workspace_id, archived_at);

ALTER TABLE inbox_item
    ADD COLUMN IF NOT EXISTS triage_status TEXT NOT NULL DEFAULT 'pending';

ALTER TABLE inbox_item
    ADD COLUMN IF NOT EXISTS snoozed_until TIMESTAMPTZ DEFAULT NULL;

ALTER TABLE inbox_item
    ADD COLUMN IF NOT EXISTS handled_at TIMESTAMPTZ DEFAULT NULL;

ALTER TABLE inbox_item
    ADD COLUMN IF NOT EXISTS dismissed_at TIMESTAMPTZ DEFAULT NULL;

ALTER TABLE inbox_item
    ADD COLUMN IF NOT EXISTS triaged_by UUID DEFAULT NULL REFERENCES "user"(id);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'inbox_item_triage_status_check'
    ) THEN
        ALTER TABLE inbox_item
            ADD CONSTRAINT inbox_item_triage_status_check
            CHECK (triage_status IN ('pending', 'handled', 'dismissed', 'snoozed'));
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_inbox_triage_visible
    ON inbox_item(workspace_id, recipient_type, recipient_id, triage_status, snoozed_until)
    WHERE archived = false;
