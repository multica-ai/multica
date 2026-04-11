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
  },
} as const;

const WORKSPACE_SCOPED_ROOTS = new Set(["workspace", "issues", "projects", "inbox", "runtimes", "tasks"]);

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
