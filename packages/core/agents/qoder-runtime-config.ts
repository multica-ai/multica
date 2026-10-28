export function qoderAgentId(config: unknown): string {
  if (!config || typeof config !== "object" || !("qoder_agent_id" in config))
    return "";
  return typeof config.qoder_agent_id === "string" ? config.qoder_agent_id : "";
}

export type QoderRepository = { url: string; ref: string };

// The repository mounted when the issue's project binds none.
export function qoderDefaultRepository(config: unknown): QoderRepository {
  const empty = { url: "", ref: "" };
  if (!config || typeof config !== "object" || !("repository" in config))
    return empty;
  const repo = config.repository;
  if (!repo || typeof repo !== "object") return empty;
  return {
    url: "url" in repo && typeof repo.url === "string" ? repo.url : "",
    ref: "ref" in repo && typeof repo.ref === "string" ? repo.ref : "",
  };
}
