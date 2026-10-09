export function qoderAgentId(config: unknown): string {
  if (!config || typeof config !== "object" || !("qoder_agent_id" in config))
    return "";
  return typeof config.qoder_agent_id === "string" ? config.qoder_agent_id : "";
}
