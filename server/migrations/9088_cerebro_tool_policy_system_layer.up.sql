-- 9088_cerebro_tool_policy_system_layer: add the System mandate layer.
--
-- FIR-1609. System is a peer principal type of User at the mandate ceiling: a
-- run with no human behind it (autopilot / system-triggered) is governed by a
-- System actor instead of a User. A System is born from a User and capped at
-- that owner's permissions, so the human ceiling is never lost. The engine
-- already resolves the system layer (toolpolicy.LayerSystem); this migration
-- lets the chain table persist rows authored at that layer.
--
-- The only schema change is widening the layer CHECK discriminator to admit
-- 'system'; storage, indexes, and the unique key are unchanged.

ALTER TABLE cerebro_tool_policy
    DROP CONSTRAINT IF EXISTS cerebro_tool_policy_layer_known;

ALTER TABLE cerebro_tool_policy
    ADD CONSTRAINT cerebro_tool_policy_layer_known
        CHECK (layer IN ('workspace', 'runtime', 'agent', 'group', 'user', 'system'));
