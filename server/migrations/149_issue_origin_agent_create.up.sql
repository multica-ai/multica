-- Extend issue.origin_type to allow an agent's ordinary `issue create` to stamp
-- the new issue with origin_type='agent_create' + origin_id=<agent_task_queue.id>
-- (the acting task that created it). This is the load-bearing link that lets
-- resolveOriginatorForIssueTask inherit the top-of-chain human originator for
-- any run derived from the new issue (assignment / squad-leader). Without it an
-- agent-created issue was left unattributed, so downstream A2A mentions from
-- those runs failed the canInvokeAgent gate against private agents (MUL-4305).
-- Mirrors the quick_create link (060) — same origin_id semantics (an
-- agent_task_queue row), different label because this is the normal create path
-- rather than the daemon quick-create flow.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create'));
