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

/**
 * `?focus=` token that scrolls the agent's General settings to its
 * conversation-starters editor and flashes it. Lives here because it is URL
 * vocabulary: `agentConversationStarters()` writes it and the page reads it,
 * and a shared constant is what stops the two from drifting apart.
 */
export const AGENT_FOCUS_CONVERSATION_STARTERS = "conversation_starters";

/**
 * My agents' work is the My issues route with `?scope=agents`. It is URL
 * vocabulary rather than stored state so every tab and window keeps its own
 * — a Cmd-click opens it beside My issues, and one tab's switch leaves the
 * others alone.
 */
const MY_AGENTS_SCOPE = "agents";

/** Whether a My issues URL's search params open My agents' work. */
export function isMyAgentsSearch(searchParams: URLSearchParams): boolean {
  return searchParams.get("scope") === MY_AGENTS_SCOPE;
}

/**
 * The Governance route's tabs. The selected tab is URL state (`?tab=`), so a
 * tab can be linked to and survives a reload. Lives here because it is URL
 * vocabulary shared by the links and the page that reads them.
 */
export const GOVERNANCE_TABS = ["goals", "budgets", "autonomy", "reviews", "log", "controls"] as const;
export type GovernanceTab = (typeof GOVERNANCE_TABS)[number];

/** The tab a `?tab=` value selects. A missing or unknown value is the default tab, goals. */
export function parseGovernanceTab(value: string | null | undefined): GovernanceTab {
  return (GOVERNANCE_TABS as readonly string[]).includes(value ?? "") ? (value as GovernanceTab) : "goals";
}

function workspaceScoped(slug: string) {
  const ws = `/${encode(slug)}`;
  return {
    root: () => `${ws}/issues`,
    usage: () => `${ws}/usage`,
    issues: () => `${ws}/issues`,
    company: () => `${ws}/company`,
    issueDetail: (id: string) => `${ws}/issues/${encode(id)}`,
    projects: () => `${ws}/projects`,
    projectDetail: (id: string) => `${ws}/projects/${encode(id)}`,
    autopilots: () => `${ws}/autopilots`,
    autopilotDetail: (id: string) => `${ws}/autopilots/${encode(id)}`,
    agents: () => `${ws}/agents`,
    // Step 1 of agent creation: choose where the agent runs.
    newAgent: () => `${ws}/agents/new`,
    // Step 2: configure the agent on the chosen runtime. A real route so a
    // half-filled form survives a refresh and can be linked to directly.
    newAgentManual: () => `${ws}/agents/new/manual`,
    // Step 3: the agent exists; offer its first uses.
    newAgentDone: (agentId: string) =>
      `${ws}/agents/new/done/${encode(agentId)}`,
    newAgentAi: () => `${ws}/agents/new/ai`,
    // One creation conversation. It is a durable object, not a step of the
    // route above: it survives leaving the studio and is resumed later, so it
    // owns an address instead of being a query param on the "start one" screen.
    newAgentAiSession: (sessionId: string) =>
      `${ws}/agents/new/ai/${encode(sessionId)}`,
    agentDetail: (id: string) => `${ws}/agents/${encode(id)}`,
    // Deep link behind "customize" in a chat's empty state: the agent's
    // General settings, scrolled to the conversation starters that produced
    // the buttons the viewer just looked at.
    agentConversationStarters: (id: string) =>
      `${ws}/agents/${encode(id)}?view=general&focus=${AGENT_FOCUS_CONVERSATION_STARTERS}`,
    memberDetail: (id: string) => `${ws}/members/${encode(id)}`,
    squads: () => `${ws}/squads`,
    knowledge: () => `${ws}/knowledge`,
    pipeline: () => `${ws}/pipeline`,
    // The pipeline board filtered to one owner squad (the Company room badge).
    pipelineForSquad: (squadId: string) => `${ws}/pipeline?squad=${encode(squadId)}`,
    // The Governance route; `tab` selects one of GOVERNANCE_TABS (default: goals).
    governance: (tab?: GovernanceTab) => (tab ? `${ws}/governance?tab=${tab}` : `${ws}/governance`),
    squadDetail: (id: string) => `${ws}/squads/${encode(id)}`,
    inbox: () => `${ws}/inbox`,
    chat: () => `${ws}/chat`,
    chatWithAgent: (agentId: string) =>
      `${ws}/chat?agent=${encode(agentId)}`,
    chatSession: (sessionId: string) =>
      `${ws}/chat?session=${encode(sessionId)}`,
    myIssues: () => `${ws}/my-issues`,
    myAgents: () => `${ws}/my-issues?scope=${MY_AGENTS_SCOPE}`,
    views: () => `${ws}/views`,
    runtimes: () => `${ws}/runtimes`,
    runtimeDetail: (id: string) => `${ws}/runtimes/${encode(id)}`,
    runtimeSettings: (machineId: string, runtimeId: string) =>
      `${ws}/runtimes/${encode(machineId)}/runtime/${encode(runtimeId)}`,
    skills: () => `${ws}/skills`,
    skillDetail: (id: string) => `${ws}/skills/${encode(id)}`,
    settings: () => `${ws}/settings`,
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
