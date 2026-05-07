type QueryKeyPart = string | number | boolean | null | undefined | Record<string, unknown>;

export type WorkspaceQueryKey = readonly QueryKeyPart[];

export const queryKeys = {
  session: {
    all: () => ["session"] as const,
    me: () => ["session", "me"] as const,
  },
  workspaces: {
    all: () => ["workspaces"] as const,
  },
  workspace: {
    all: () => ["workspace"] as const,
    detail: (workspaceId: string) => ["workspace", workspaceId] as const,
    members: (workspaceId: string) => ["workspace", workspaceId, "members"] as const,
    agents: (workspaceId: string) => ["workspace", workspaceId, "agents"] as const,
    labels: (workspaceId: string) => ["workspace", workspaceId, "labels"] as const,
    skills: (workspaceId: string) => ["workspace", workspaceId, "skills"] as const,
    skillDetail: (workspaceId: string, skillId: string) =>
      ["workspace", workspaceId, "skills", skillId] as const,
  },
  issues: {
    all: () => ["issues"] as const,
    lists: () => ["issues", "lists"] as const,
    list: (workspaceId: string, params?: Record<string, unknown>) =>
      ["issues", "lists", workspaceId, params ?? {}] as const,
    detail: (issueId: string) => ["issues", "detail", issueId] as const,
    timeline: (issueId: string) => ["issues", "detail", issueId, "timeline"] as const,
    reactions: (issueId: string) => ["issues", "detail", issueId, "reactions"] as const,
    subscribers: (issueId: string) =>
      ["issues", "detail", issueId, "subscribers"] as const,
  },
  projects: {
    all: () => ["projects"] as const,
    lists: () => ["projects", "lists"] as const,
    list: (workspaceId: string, params?: Record<string, unknown>) =>
      ["projects", "lists", workspaceId, params ?? {}] as const,
    detail: (workspaceId: string, projectId: string) =>
      ["projects", "detail", workspaceId, projectId] as const,
  },
  inbox: {
    all: (workspaceId: string) => ["inbox", workspaceId] as const,
  },
  runtimes: {
    all: (workspaceId: string) => ["runtimes", workspaceId] as const,
  },
  tasks: {
    byIssue: (issueId: string) => ["tasks", "issue", issueId] as const,
    activeByIssue: (issueId: string) => ["tasks", "issue", issueId, "active"] as const,
    messages: (taskId: string) => ["tasks", "detail", taskId, "messages"] as const,
  },
  settings: {
    all: () => ["settings"] as const,
    tokens: () => ["settings", "tokens"] as const,
    notificationPreferences: () => ["settings", "notification-preferences"] as const,
    aiSettings: (workspaceId: string) => ["settings", "ai", workspaceId] as const,
  },
  timeTracking: {
    all: () => ["time-tracking"] as const,
    current: (workspaceId: string) => ["time-tracking", "current", workspaceId] as const,
    // Broad key used for invalidation (catches all entries sub-keys for the workspace).
    entries: (workspaceId: string) => ["time-tracking", "entries", workspaceId] as const,
    // Specific key used when fetching with params (since/until or limit/offset).
    // Nested under entries(workspaceId) so invalidating the parent clears all variants.
    entriesParams: (workspaceId: string, params: Record<string, unknown>) =>
      ["time-tracking", "entries", workspaceId, params] as const,
    issueEntries: (issueId: string) => ["time-tracking", "issue", issueId] as const,
  },
} as const;

const WORKSPACE_SCOPED_ROOTS = new Set(["workspace", "issues", "projects", "inbox", "runtimes", "tasks", "time-tracking"]);

export function isWorkspaceScopedQueryKey(queryKey: readonly unknown[]): boolean {
  const root = queryKey[0];
  return typeof root === "string" && WORKSPACE_SCOPED_ROOTS.has(root);
}

export function queryKeyIncludesWorkspace(
  queryKey: readonly unknown[],
  workspaceId: string,
): boolean {
  return queryKey.includes(workspaceId);
}
