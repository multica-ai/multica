-- 9052_cerebro_tool_policy_workspace_layer: add the workspace root layer.
--
-- FIR-2284 Bid 5 (the five surfaces). The chain gains a workspace-wide root
-- layer below runtime: Workspace -> Runtime -> Agent -> Group -> User. A
-- workspace-layer row carries subject_id = the workspace's own id, so the
-- per-tool default authored under Settings applies to every runtime, agent,
-- group, and user beneath it (each can only tighten it further).
--
-- The only schema change is widening the layer CHECK discriminator to admit
-- 'workspace'; storage, indexes, and the unique key are unchanged.

ALTER TABLE cerebro_tool_policy
    DROP CONSTRAINT IF EXISTS cerebro_tool_policy_layer_known;

ALTER TABLE cerebro_tool_policy
    ADD CONSTRAINT cerebro_tool_policy_layer_known
        CHECK (layer IN ('workspace', 'runtime', 'agent', 'group', 'user'));
