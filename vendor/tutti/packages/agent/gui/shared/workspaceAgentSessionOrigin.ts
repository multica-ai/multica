import { AGENT_CONVERSATION_LOCAL_ORIGIN } from "@tutti-os/agent-activity-core";

export const WORKSPACE_AGENT_ACTIVITY_RUNTIME_SESSION_ORIGIN =
  AGENT_CONVERSATION_LOCAL_ORIGIN;

export function isWorkspaceAgentActivityRuntimeSessionOrigin(
  sessionOrigin: string | null | undefined
): boolean {
  const normalized = sessionOrigin?.trim() ?? "";
  return (
    normalized === "" ||
    normalized === WORKSPACE_AGENT_ACTIVITY_RUNTIME_SESSION_ORIGIN
  );
}
