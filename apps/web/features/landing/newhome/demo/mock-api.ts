// A stand-in ApiClient for the landing-page product demo. Returns canned,
// server-shaped responses from mock-data so the real product components run
// with zero backend. Installed via setApiInstance() (the global injection
// seam in @multica/core/api). Any method not implemented here resolves to
// `undefined` via the Proxy fallback, so unanticipated calls never throw.

import type { ApiClient } from "@multica/core/api";
import {
  AGENTS,
  EXEC_LOG,
  ISSUES,
  MEMBERS,
  PULL_REQUESTS,
  RUNNING_TASKS,
  TIMELINE,
  TRANSCRIPT_BY_ISSUE,
  WORKSPACE,
  createMockIssue,
  patchIssue,
} from "./mock-data";

// Every task (snapshot + per-issue execution log), so a transcript request for
// any task id can resolve which issue (and thus which transcript) it belongs to.
const ALL_TASKS = [...RUNNING_TASKS, ...Object.values(EXEC_LOG).flat()];

type AnyParams = Record<string, unknown> | undefined;

const handlers: Record<string, (...args: any[]) => Promise<unknown>> = {
  // Keep the demo logged-out: the landing's auth init must not think a user
  // is signed in (that would redirect away from the marketing page).
  getMe: () => Promise.reject(new Error("demo: unauthenticated")),
  getBaseUrl: () => "" as unknown as Promise<unknown>,

  listWorkspaces: () => Promise.resolve([WORKSPACE]),
  getWorkspace: () => Promise.resolve(WORKSPACE),
  listMembers: () => Promise.resolve(MEMBERS),
  listAgents: () => Promise.resolve(AGENTS),
  listSquads: () => Promise.resolve([]),
  listRuntimes: () => Promise.resolve([]),
  getAgent: (id: string) =>
    Promise.resolve(AGENTS.find((a) => a.id === id) ?? AGENTS[0]),

  // Agent transcript (live run log) — resolve the task's issue, return its
  // transcript with seq / task_id / issue_id stamped on each message.
  listTaskMessages: (taskId: string) => {
    const task = ALL_TASKS.find((t) => t.id === taskId);
    const tmpl = task ? TRANSCRIPT_BY_ISSUE[task.issue_id] : undefined;
    if (!task || !tmpl) return Promise.resolve([]);
    return Promise.resolve(
      tmpl.map((m, i) => ({
        ...m,
        seq: i,
        task_id: task.id,
        issue_id: task.issue_id,
      })),
    );
  },
  listSkills: () => Promise.resolve([]),
  getAssigneeFrequency: () => Promise.resolve([]),

  listIssues: (params: AnyParams) => {
    const status = params?.status as string | undefined;
    const issues = status ? ISSUES.filter((i) => i.status === status) : [...ISSUES];
    return Promise.resolve({ issues, total: issues.length });
  },
  listGroupedIssues: () => Promise.resolve({ groups: [] }),
  getIssue: (id: string) => {
    const issue = ISSUES.find((i) => i.id === id);
    return issue
      ? Promise.resolve(issue)
      : Promise.reject(new Error("demo: issue not found"));
  },
  getChildIssueProgress: () => Promise.resolve({ progress: [] }),
  listChildIssues: () => Promise.resolve({ issues: [] }),
  listChildrenByParents: () => Promise.resolve({ issues: [] }),
  listTimeline: (issueId: string) => Promise.resolve(TIMELINE[issueId] ?? []),
  listComments: () => Promise.resolve([]),
  listIssueSubscribers: () => Promise.resolve([]),
  listAttachments: () => Promise.resolve([]),
  getIssueUsage: () =>
    Promise.resolve({ total_tokens: 0, total_cost_usd: 0, runs: 0 }),
  listProjects: () => Promise.resolve({ projects: [] }),
  listLabels: () => Promise.resolve({ labels: [] }),
  listIssuePullRequests: (issueId: string) =>
    Promise.resolve({ pull_requests: PULL_REQUESTS[issueId] ?? [] }),
  listTasksByIssue: (issueId: string) =>
    Promise.resolve(EXEC_LOG[issueId] ?? []),
  listAgentTasks: () => Promise.resolve([]),
  getAgentTaskSnapshot: () => Promise.resolve(RUNNING_TASKS),
  getActiveTasksForIssue: (issueId: string) =>
    Promise.resolve({
      tasks: RUNNING_TASKS.filter((t) => t.issue_id === issueId),
    }),

  // Writes: mutate the in-memory issue so drag-to-change-status persists
  // across the refetch that react-query fires after a mutation settles.
  updateIssue: (id: string, data: AnyParams) => {
    const updated = patchIssue(id, (data ?? {}) as Record<string, never>);
    return Promise.resolve(updated ?? ISSUES.find((i) => i.id === id));
  },
  // Create-issue flow: build a card from the dialog input and add it.
  createIssue: (data: AnyParams) =>
    Promise.resolve(createMockIssue((data ?? {}) as { title: string })),
  quickCreateIssue: (data: AnyParams) => {
    const d = (data ?? {}) as { prompt?: string; agent_id?: string };
    createMockIssue({
      title: (d.prompt || "New agent task").slice(0, 80),
      status: "todo",
      assignee_type: d.agent_id ? "agent" : undefined,
      assignee_id: d.agent_id,
    });
    return Promise.resolve({ task_id: "task-new" });
  },
};

export function createMockApi(): ApiClient {
  const target = {} as Record<string, unknown>;
  return new Proxy(target, {
    get(_t, prop: string) {
      if (prop in handlers) return handlers[prop];
      // Unknown method → resolve to undefined so no call ever throws.
      return () => Promise.resolve(undefined);
    },
  }) as unknown as ApiClient;
}
