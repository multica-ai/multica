export type AgentCenterSection = 'roles' | 'teams';

export function agentCenterPath(
  workspaceSlug: string,
  section: AgentCenterSection,
): string {
  const root = `/${encodeURIComponent(workspaceSlug)}/agents`;
  return section === 'teams' ? `${root}/teams` : root;
}
