UPDATE issue_wakeup SET paused_reason = NULL
WHERE paused_reason IN ('issue_run_limit', 'issue_agent_run_limit');

ALTER TABLE issue_wakeup
    DROP CONSTRAINT issue_wakeup_paused_reason_check,
    ADD CONSTRAINT issue_wakeup_paused_reason_check CHECK (paused_reason IN ('max_fires', 'loop', 'rate'));
