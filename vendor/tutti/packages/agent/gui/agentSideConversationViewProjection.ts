import type { AgentActivityEphemeralConversationProjection } from "@tutti-os/agent-activity-core";
import type { AgentSideConversationState } from "./agentSideConversationRuntime";
import type { AgentConversationVM } from "./shared/agentConversation/contracts/agentConversationVM";
import { projectAgentActivitySessionToConversationVM } from "./shared/agentConversation/projection/workspaceAgentMessageProjection";

export interface AgentSideConversationViewState extends AgentSideConversationState {
  conversation: AgentConversationVM | null;
}

export function projectAgentSideConversationVM(
  projection: AgentActivityEphemeralConversationProjection
): AgentConversationVM | null {
  const session = projection.activitySnapshot.sessions[0];
  return session
    ? projectAgentActivitySessionToConversationVM({
        activitySnapshot: projection.activitySnapshot,
        agentSessionId: session.agentSessionId,
        messages: projection.messages,
        sessionTurns: projection.sessionTurns,
        workspaceRoot: session.cwd || null
      })
    : null;
}

export function projectAgentSideConversationViewState(
  state: AgentSideConversationState | null
): AgentSideConversationViewState | null {
  return state
    ? {
        ...state,
        conversation: projectAgentSideConversationVM(state.projection)
      }
    : null;
}
