-- Workspace orchestrator agent: optional FK to a workspace agent that should
-- be woken up when peer agents post comments on issues in this workspace.
--
-- The orchestrator pattern (Hermes routing work to coding agents, then
-- reacting to their completion / clarification comments to drive the issue
-- forward) needs an explicit "this is the orchestrator" pointer so the
-- comment trigger can route deterministically without hardcoding a name.
--
-- Nullable: NULL means "no orchestrator configured", which is the existing
-- behavior (no comment-triggered orchestrator wake-ups). Existing workspaces
-- inherit NULL — zero behavior change until an admin opts in.
--
-- ON DELETE SET NULL: archiving / deleting the orchestrator agent simply
-- clears the pointer; the workspace doesn't lose its other settings.
ALTER TABLE workspace
  ADD COLUMN orchestrator_agent_id UUID
  REFERENCES agent(id) ON DELETE SET NULL;
