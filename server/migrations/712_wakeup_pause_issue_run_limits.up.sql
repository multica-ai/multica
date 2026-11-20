ALTER TABLE issue_wakeup
    DROP CONSTRAINT issue_wakeup_paused_reason_check,
    ADD CONSTRAINT issue_wakeup_paused_reason_check CHECK (
        paused_reason IN ('max_fires', 'loop', 'rate', 'issue_run_limit', 'issue_agent_run_limit')
    );
