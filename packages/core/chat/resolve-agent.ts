import { isAgentRuntimeBound } from "../agents/runtime-binding";
import type { Agent } from "../types";

export function workspaceDefaultChatAgentId(settings: unknown): string | null {
  if (!settings || typeof settings !== "object" || Array.isArray(settings)) return null;
  const id = (settings as Record<string, unknown>).default_chat_agent_id;
  return typeof id === "string" && id.length > 0 ? id : null;
}

// Resolution has no side effects: a changing workspace fallback must not become
// a personal preference, start a session, or touch the composer's draft.
export function resolveChatAgent(input: {
  agents: Agent[];
  availableAgents: Agent[];
  agentsSettled: boolean;
  sessionId: string | null;
  sessionAgentId: string | null;
  explicitAgentId?: string | null;
  selectedAgentId: string | null;
  defaultAgentId: string | null;
}): Agent | null {
  if (input.sessionId) {
    return input.agents.find((agent) => agent.id === input.sessionAgentId) ?? null;
  }
  if (!input.agentsSettled) return null;
  if (input.explicitAgentId) {
    return input.availableAgents.find((agent) => agent.id === input.explicitAgentId) ?? null;
  }
  return input.availableAgents.find((agent) => agent.id === input.selectedAgentId)
    ?? input.availableAgents.find((agent) => agent.id === input.defaultAgentId && isAgentRuntimeBound(agent))
    ?? input.availableAgents[0]
    ?? null;
}
