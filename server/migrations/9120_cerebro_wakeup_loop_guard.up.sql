-- FIR-2679 Spor 1a: wakeup loop-guard. Caps how many self-wakeups an agent may
-- chain on the same issue without a human replying in between, so a run can no
-- longer wake itself in an endless loop (worst observed: 18 re-arms in 22h). The
-- streak resets whenever a human (member) comments on the issue; once the cap is
-- reached the create call is rejected and the agent is told to post a status /
-- question to the human instead of scheduling another wakeup.
--
-- NOT NULL with a sane default so existing rows and the "no row -> default" path
-- in the settings handler behave identically to today. 0 = guard disabled.
ALTER TABLE cerebro_workspace_settings
    ADD COLUMN IF NOT EXISTS wakeup_max_consecutive_loops INT NOT NULL DEFAULT 5;
