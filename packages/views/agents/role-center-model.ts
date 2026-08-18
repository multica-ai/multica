/**
 * The Tag Agents surface is a role center, not a second task workspace or
 * an advanced runtime administration console. Keep these destinations in one
 * pure model so route wrappers and shared views cannot drift apart.
 */
export const AGENT_ROLE_CENTER_TABS = [
  "overview",
  "skills",
  "instructions",
  "general",
] as const;

export type AgentRoleCenterTab = (typeof AGENT_ROLE_CENTER_TABS)[number];

export function agentRoleCenterCreatePath(workspacePath: string): string {
  return `${workspacePath}/agents/new/manual`;
}

export function agentRoleCenterWorkPath(workspacePath: string): string {
  return `${workspacePath}/issues`;
}
