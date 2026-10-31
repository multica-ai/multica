export function claudeAgentId(config: unknown): string {
  if (!config || typeof config !== "object" || !("claude_agent_id" in config))
    return "";
  return typeof config.claude_agent_id === "string" ? config.claude_agent_id : "";
}
