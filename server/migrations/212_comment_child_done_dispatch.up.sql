ALTER TABLE comment
    ADD COLUMN child_done_barrier_key TEXT,
    ADD COLUMN child_done_target_type TEXT
        CHECK (child_done_target_type IN ('agent', 'squad')),
    ADD COLUMN child_done_target_id UUID,
    ADD COLUMN child_done_origin_task_id UUID,
    ADD COLUMN child_done_dispatch_status TEXT
        CHECK (child_done_dispatch_status IN ('queued', 'dispatched', 'skipped')),
    ADD COLUMN child_done_available_at TIMESTAMPTZ,
    ADD COLUMN child_done_lease_token UUID,
    ADD COLUMN child_done_lease_expires_at TIMESTAMPTZ,
    ADD COLUMN child_done_dispatch_attempts INTEGER NOT NULL DEFAULT 0
        CHECK (child_done_dispatch_attempts >= 0),
    ADD COLUMN child_done_dispatch_error TEXT;
