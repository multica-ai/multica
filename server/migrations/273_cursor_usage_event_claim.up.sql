-- Cursor Dashboard events are account-wide and have no task id. This ledger
-- prevents daemons sharing one Cursor login from attributing an event to more
-- than one task. Both keys are opaque SHA-256 digests computed by the daemon.
--
-- No foreign keys (repo rule). Workspace deletion removes rows explicitly.
CREATE TABLE cursor_usage_event_claim (
    account_key    TEXT NOT NULL,
    occurrence_key TEXT NOT NULL,
    task_id        UUID NOT NULL,
    workspace_id   UUID NOT NULL
);
