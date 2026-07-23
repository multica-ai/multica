-- CEREBRO-PATCH(model-usage-event-optional-issue-scope): FIR-3337 keeps
-- canonical usage for chat and run-only Autopilot tasks that have no issue.
ALTER TABLE model_usage_event ALTER COLUMN issue_id DROP NOT NULL;
