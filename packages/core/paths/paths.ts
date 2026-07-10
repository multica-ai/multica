/**
 * Centralized URL path builder. All navigation in shared packages (packages/views)
 * MUST go through this module — no hardcoded string paths.
 *
 * Two kinds of paths:
 *  - workspace-scoped: paths.workspace(slug).xxx() — carry workspace in URL
 *  - global: paths.login(), paths.newWorkspace(), paths.invite(id) — pre-workspace routes
 *
 * Why pure functions + builder pattern:
 *  - Changing a route shape (e.g. adding workspace slug prefix) becomes a single-file edit
 *  - IDs are always URL-encoded here so callers can't forget
 *  - Zero runtime deps means this module is safe in Node (tests) and browsers
 */

const encode = (id: string) => encodeURIComponent(id);

function workspaceScoped(slug: string) {
  const ws = `/${encode(slug)}`;
  return {
    root: () => `${ws}/issues`,
    usage: () => `${ws}/usage`,
    issues: () => `${ws}/issues`,
    // Issue detail is identifier-first (Linear-style /issue/NAI-3): the space
    // rides in the identifier, never in a nested path segment, so moving an
    // issue between spaces can't orphan the URL (old identifiers keep
    // resolving via the server-side alias). The same route accepts a UUID —
    // internal navigation passes ids, shared links pass identifiers.
    issueDetail: (idOrIdentifier: string) => `${ws}/issue/${encode(idOrIdentifier)}`,
    // Space-scoped surfaces, addressed by space key (readable, stable: keys
    // freeze once a space has issues) — /space/ENG/issues, Linear-style.
    spaceIssues: (key: string) => `${ws}/space/${encode(key)}/issues`,
    spaceProjects: (key: string) => `${ws}/space/${encode(key)}/projects`,
    spaceAutopilots: (key: string) => `${ws}/space/${encode(key)}/autopilots`,
    spaceDetail: (key: string) => `${ws}/space/${encode(key)}`,
    spaceSettings: (key: string) => `${ws}/space/${encode(key)}/settings`,
    spacesDirectory: () => `${ws}/spaces`,
    // Static sibling of spaceDetail — "new" is a reserved space key
    // (RESERVED_SPACE_KEYS) precisely so it can never collide with this route.
    spaceNew: () => `${ws}/space/new`,
    projects: () => `${ws}/projects`,
    projectDetail: (id: string) => `${ws}/projects/${encode(id)}`,
    autopilots: () => `${ws}/autopilots`,
    autopilotDetail: (id: string) => `${ws}/autopilots/${encode(id)}`,
    agents: () => `${ws}/agents`,
    newAgent: () => `${ws}/agents/new`,
    agentDetail: (id: string) => `${ws}/agents/${encode(id)}`,
    memberDetail: (id: string) => `${ws}/members/${encode(id)}`,
    squads: () => `${ws}/squads`,
    squadDetail: (id: string) => `${ws}/squads/${encode(id)}`,
    inbox: () => `${ws}/inbox`,
    chat: () => `${ws}/chat`,
    myIssues: () => `${ws}/my-issues`,
    runtimes: () => `${ws}/runtimes`,
    runtimeDetail: (id: string) => `${ws}/runtimes/${encode(id)}`,
    runtimeSettings: (machineId: string, runtimeId: string) =>
      `${ws}/runtimes/${encode(machineId)}/runtime/${encode(runtimeId)}`,
    skills: () => `${ws}/skills`,
    skillDetail: (id: string) => `${ws}/skills/${encode(id)}`,
    settings: () => `${ws}/settings`,
    settingsSection: (scope: string, page: string) =>
      `${ws}/settings/${encode(scope)}/${encode(page)}`,
    attachmentPreview: (id: string) => `${ws}/attachments/${encode(id)}/preview`,
  };
}

export const paths = {
  workspace: workspaceScoped,

  // Global (pre-workspace) routes
  login: () => "/login",
  newWorkspace: () => "/workspaces/new",
  invite: (id: string) => `/invite/${encode(id)}`,
  invitations: () => "/invitations",
  onboarding: () => "/onboarding",
  authCallback: () => "/auth/callback",
  root: () => "/",
};

export type WorkspacePaths = ReturnType<typeof workspaceScoped>;

// Prefixes — not slug names — because we match against full URL paths.
// A path is global if it equals or begins with any of these.
// Note: `/workspaces/` (trailing slash) is the prefix — `workspaces` is reserved,
// so any path starting with `/workspaces/...` is system-owned, not user-owned.
const GLOBAL_PREFIXES = ["/login", "/workspaces/", "/invite/", "/invitations", "/onboarding", "/auth/", "/logout", "/signup"];

export function isGlobalPath(path: string): boolean {
  return GLOBAL_PREFIXES.some((p) => path === p || path.startsWith(p));
}
