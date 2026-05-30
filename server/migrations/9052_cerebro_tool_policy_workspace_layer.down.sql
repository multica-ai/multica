-- Revert 9052: drop any workspace-layer rows, then narrow the layer CHECK back
-- to the original four layers so the constraint can be re-added cleanly.

DELETE FROM cerebro_tool_policy WHERE layer = 'workspace';

ALTER TABLE cerebro_tool_policy
    DROP CONSTRAINT IF EXISTS cerebro_tool_policy_layer_known;

ALTER TABLE cerebro_tool_policy
    ADD CONSTRAINT cerebro_tool_policy_layer_known
        CHECK (layer IN ('runtime', 'agent', 'group', 'user'));
