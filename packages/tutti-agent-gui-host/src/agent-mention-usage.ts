export const AGENT_MENTION_USAGE_KEY = "agent-mention-usage:v1";
export const AGENT_MENTION_SELECTION_KEY = "agent-mention-selection:v1";

export type AgentMentionUsage = Record<string, { count: number; lastUsedAt: number }>;

export function parseAgentMentionUsage(raw: string | null): AgentMentionUsage {
  const agents: AgentMentionUsage = Object.create(null);
  if (!raw) return agents;
  try {
    const value = JSON.parse(raw);
    if (value?.version !== 1 || !value.agents || typeof value.agents !== "object" || Array.isArray(value.agents)) return agents;
    for (const [id, entry] of Object.entries(value.agents)) {
      if (!entry || typeof entry !== "object" || !("count" in entry) || !("lastUsedAt" in entry)) continue;
      if (typeof entry.count === "number" && Number.isSafeInteger(entry.count) && entry.count > 0 && typeof entry.lastUsedAt === "number" && Number.isFinite(entry.lastUsedAt) && entry.lastUsedAt >= 0) {
        agents[id] = { count: entry.count, lastUsedAt: entry.lastUsedAt };
      }
    }
  } catch { /* Invalid local preference data starts a new selection history. */ }
  return agents;
}
