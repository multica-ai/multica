-- Revert the on_behalf_of actor layer. Any rows authored at that layer must be
-- dropped first so the narrower CHECK can be re-applied.

DELETE FROM cerebro_tool_policy WHERE layer = 'on_behalf_of';

ALTER TABLE cerebro_tool_policy
    DROP CONSTRAINT IF EXISTS cerebro_tool_policy_layer_known;

ALTER TABLE cerebro_tool_policy
    ADD CONSTRAINT cerebro_tool_policy_layer_known
        CHECK (layer IN ('workspace', 'runtime', 'agent', 'group', 'user', 'system'));
