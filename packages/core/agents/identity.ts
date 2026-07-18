import type { Agent } from "../types";

/**
 * GitLab comment-attribution personas are identity shells, not workers.
 * They stay in the agent list only so comment author name/avatar resolve;
 * assign, mention, chat, and Agents-page surfaces must exclude them.
 */
export function isGitLabPersonaAgent(
  agent: Pick<Agent, "system_key">,
): boolean {
  const key = agent.system_key;
  return typeof key === "string" && key.startsWith("gitlab:");
}
