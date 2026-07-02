-- 9110_cerebro_tool_policy_on_behalf_of_layer: add the on_behalf_of actor layer.
--
-- FIR-2441. on_behalf_of is the delegated member (the task initiator) as a real,
-- tighten-only actor level in the tool-policy chain, distinct from the agent
-- owner at 'user'. A rule authored at this layer can deny / ask-restrict a tool
-- for the person who drives the agent, across every agent they drive, but can
-- never widen access (the Resolve chain is tighten-only for this layer). The
-- engine already resolves it (toolpolicy.LayerOnBehalfOf); this migration lets
-- the chain table persist rows authored at that layer.
--
-- The only schema change is widening the layer CHECK discriminator to admit
-- 'on_behalf_of'; storage, indexes, and the unique key are unchanged.

ALTER TABLE cerebro_tool_policy
    DROP CONSTRAINT IF EXISTS cerebro_tool_policy_layer_known;

ALTER TABLE cerebro_tool_policy
    ADD CONSTRAINT cerebro_tool_policy_layer_known
        CHECK (layer IN ('workspace', 'runtime', 'agent', 'group', 'user', 'system', 'on_behalf_of'));
