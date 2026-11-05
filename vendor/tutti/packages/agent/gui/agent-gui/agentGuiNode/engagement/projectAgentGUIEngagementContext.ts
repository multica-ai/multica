import type {
  AgentGUIRailViewModel,
  AgentGUIShellViewModel
} from "../model/agentGuiNodeTypes";
import type { AgentGUIEngagementContext } from "./agentGUIEngagement.types";

export function projectAgentGUIEngagementContext(
  {
    shell,
    rail
  }: { rail: AgentGUIRailViewModel; shell: AgentGUIShellViewModel },
  composerReady: boolean
): { context: AgentGUIEngagementContext; contextKey: string } {
  const agentSessionId = rail.activeConversationId;
  const provider =
    rail.activeConversation?.provider ??
    rail.selectedAgentTarget.provider ??
    shell.data.provider;
  const agentTargetId =
    rail.activeConversation?.agentTargetId ??
    rail.selectedAgentTarget.agentTargetId ??
    rail.selectedAgentTarget.targetId ??
    null;
  return {
    context: {
      agentSessionId,
      agentTargetId,
      composerReady,
      conversationState: agentSessionId ? "existing" : "new",
      provider
    },
    contextKey: createAgentGUIEngagementContextKey({
      agentSessionId,
      agentTargetId,
      provider
    })
  };
}

export function createAgentGUIEngagementContextKey(input: {
  agentSessionId: string | null;
  agentTargetId: string | null;
  provider: string;
}): string {
  return JSON.stringify([
    input.agentSessionId ? "session" : "home",
    input.agentSessionId,
    input.provider,
    input.agentTargetId
  ]);
}
