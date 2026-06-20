-- Revert the System mandate layer. Any rows authored at the system layer must
-- be dropped first so the narrower CHECK can be re-applied.

DELETE FROM cerebro_tool_policy WHERE layer = 'system';

ALTER TABLE cerebro_tool_policy
    DROP CONSTRAINT IF EXISTS cerebro_tool_policy_layer_known;

ALTER TABLE cerebro_tool_policy
    ADD CONSTRAINT cerebro_tool_policy_layer_known
        CHECK (layer IN ('workspace', 'runtime', 'agent', 'group', 'user'));
