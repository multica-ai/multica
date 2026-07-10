-- 9122_cerebro_tool_policy_disable_setting: add the workspace-only 'disable' setting.
--
-- FIR-2351 follow-up (product decision 2026-07-06, Jesper): after a workspace
-- Deny became an openable default (9d6ecace, #2113), an owner/admin still
-- needs a way to make ONE specific permission a hard, unopenable floor for
-- everyone but themselves — exactly like the existing credential/sandbox/repo
-- floors, but choosable per permission instead of hardcoded. 'disable' is that
-- third workspace-layer state: no Group/User/Agent Allow can loosen it (see
-- resolveOpenable's workspaceDisabled handling in chain.go). It is meaningful
-- ONLY at layer='workspace' — the write path (RequireToolPolicyWritePolicy)
-- already restricts workspace rows to owner/admin, and validSetting()/Set()
-- reject 'disable' at any other layer in Go, but the CHECK constraint below
-- enforces the same restriction as defense in depth at the data layer.

ALTER TABLE cerebro_tool_policy
    DROP CONSTRAINT IF EXISTS cerebro_tool_policy_setting_known;

ALTER TABLE cerebro_tool_policy
    ADD CONSTRAINT cerebro_tool_policy_setting_known
        CHECK (setting IN ('inherit', 'allow', 'ask', 'deny', 'disable'));

ALTER TABLE cerebro_tool_policy
    DROP CONSTRAINT IF EXISTS cerebro_tool_policy_disable_workspace_only;

ALTER TABLE cerebro_tool_policy
    ADD CONSTRAINT cerebro_tool_policy_disable_workspace_only
        CHECK (setting != 'disable' OR layer = 'workspace');
