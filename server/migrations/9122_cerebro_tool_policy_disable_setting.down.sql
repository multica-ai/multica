-- Revert 9122: drop any disable rows, then narrow the setting CHECK back to
-- the original four settings so the constraint can be re-added cleanly.

DELETE FROM cerebro_tool_policy WHERE setting = 'disable';

ALTER TABLE cerebro_tool_policy
    DROP CONSTRAINT IF EXISTS cerebro_tool_policy_disable_workspace_only;

ALTER TABLE cerebro_tool_policy
    DROP CONSTRAINT IF EXISTS cerebro_tool_policy_setting_known;

ALTER TABLE cerebro_tool_policy
    ADD CONSTRAINT cerebro_tool_policy_setting_known
        CHECK (setting IN ('inherit', 'allow', 'ask', 'deny'));
