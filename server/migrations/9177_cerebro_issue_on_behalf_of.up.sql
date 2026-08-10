-- CEREBRO-PATCH(issue-on-behalf-of-column): FIR-4930 — explicit human origin on an issue.
--
-- Until now "on behalf of" was derived only: issue.origin_type='agent_task'
-- → agent_task_queue.original_user_id. For an autopilot that fans work out to
-- many owners that always resolves to the autopilot's creator, so every issue
-- the agent files is attributed to (and inbox-notified to) the wrong human.
--
-- This column is the explicit override. NULL keeps the derived behaviour, so
-- every existing issue is byte-identical after this migration.
ALTER TABLE issue
    ADD COLUMN IF NOT EXISTS on_behalf_of_user_id UUID REFERENCES "user"(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_issue_on_behalf_of_user_id
    ON issue (workspace_id, on_behalf_of_user_id)
    WHERE on_behalf_of_user_id IS NOT NULL;
