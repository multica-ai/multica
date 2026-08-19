export type AgentCenterSection = 'roles' | 'teams';

export function agentCenterPath(
  workspaceSlug: string,
  section: AgentCenterSection,
): string {
  const root = `/${encodeURIComponent(workspaceSlug)}/agents`;
  return section === 'teams' ? `${root}/teams` : root;
}

/**
 * Keeps the reused Squads views inside the Agents product surface without
 * changing their shared Core/API vocabulary.
 */
export function remapAgentCenterPath(path: string): string {
  const url = new URL(path, 'https://multica.local');
  const segments = url.pathname.split('/').filter(Boolean);
  if (segments.length < 2) return path;

  const [workspaceSlug, section, objectId] = segments;
  if (section === 'squads') {
    url.pathname = objectId
      ? `/${workspaceSlug}/agents/teams/${objectId}`
      : `/${workspaceSlug}/agents/teams`;
  } else if (section === 'agents' && objectId === 'new' && segments.length === 3) {
    url.pathname = `/${workspaceSlug}/agents/new/manual`;
  } else {
    return path;
  }

  return `${url.pathname}${url.search}${url.hash}`;
}
