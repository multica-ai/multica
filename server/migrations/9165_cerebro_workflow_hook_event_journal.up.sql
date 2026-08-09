CREATE TABLE IF NOT EXISTS cerebro_workflow_hook_event_journal (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    event_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    schema_version INTEGER NOT NULL DEFAULT 1 CHECK (schema_version = 1),
    event_hash TEXT NOT NULL,
    replay_event JSONB NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT (now() + interval '7 days'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, event_hash),
    CHECK (expires_at <= occurred_at + interval '7 days')
);

CREATE INDEX IF NOT EXISTS hook_event_journal_workspace_type_time
    ON cerebro_workflow_hook_event_journal
    (workspace_id, event_type, occurred_at DESC, id DESC);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'hook_draft_revision_workspace_id_unique'
    ) THEN
        ALTER TABLE cerebro_workflow_hook_draft_revision
            ADD CONSTRAINT hook_draft_revision_workspace_id_unique
            UNIQUE (workspace_id, id);
    END IF;
END
$$;

ALTER TABLE cerebro_workflow_hook_run
    ALTER COLUMN policy_id DROP NOT NULL,
    ADD COLUMN IF NOT EXISTS draft_revision_id UUID;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'hook_run_draft_revision_fk'
    ) THEN
        ALTER TABLE cerebro_workflow_hook_run
            ADD CONSTRAINT hook_run_draft_revision_fk
                FOREIGN KEY (workspace_id, draft_revision_id)
                REFERENCES cerebro_workflow_hook_draft_revision (workspace_id, id)
                ON DELETE CASCADE;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'hook_run_policy_or_draft_check'
    ) THEN
        ALTER TABLE cerebro_workflow_hook_run
            ADD CONSTRAINT hook_run_policy_or_draft_check
                CHECK ((policy_id IS NOT NULL) <> (draft_revision_id IS NOT NULL));
    END IF;
END
$$;

CREATE TABLE IF NOT EXISTS cerebro_workflow_hook_test_evidence (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    family_id UUID NOT NULL,
    draft_revision_id UUID NOT NULL,
    event_journal_id UUID NOT NULL,
    hook_run_id UUID NOT NULL REFERENCES cerebro_workflow_hook_run(id) ON DELETE CASCADE,
    qualified_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, draft_revision_id),
    FOREIGN KEY (workspace_id, family_id, draft_revision_id)
        REFERENCES cerebro_workflow_hook_draft_revision (workspace_id, family_id, id)
        ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, event_journal_id)
        REFERENCES cerebro_workflow_hook_event_journal (workspace_id, id)
        ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS hook_test_evidence_family_revision
    ON cerebro_workflow_hook_test_evidence
    (workspace_id, family_id, draft_revision_id);
