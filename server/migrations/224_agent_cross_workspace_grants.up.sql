-- cross_workspace_ids is an owner/admin-configured allow-list of OTHER
-- workspace IDs a running agent task may request a scoped mat_ token for.
-- It exists so an agent whose job is cross-workspace routing (e.g. reading
-- issues in a second workspace) never needs the daemon owner's full mul_
-- PAT: the daemon-managed CLI mints a second task_token row bound to
-- (task_id, agent_id, one target workspace_id) instead, following the same
-- MUL-2600 task-token model already used for the agent's primary workspace.
-- Empty (the default) means no cross-workspace access at all.
ALTER TABLE agent ADD COLUMN cross_workspace_ids UUID[] NOT NULL DEFAULT '{}';
