import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { ListIssuesParams } from "../types";
import type { FileNode, GitFileStatus } from "../types/events";

export const issueKeys = {
  all: (wsId: string) => ["issues", wsId] as const,
  list: (wsId: string) => [...issueKeys.all(wsId), "list"] as const,
  /** All "my issues" queries — use for bulk invalidation. */
  myAll: (wsId: string) => [...issueKeys.all(wsId), "my"] as const,
  /** Per-scope "my issues" list with filter identity baked into the key. */
  myList: (wsId: string, scope: string, filter: MyIssuesFilter) =>
    [...issueKeys.myAll(wsId), scope, filter] as const,
  detail: (wsId: string, id: string) =>
    [...issueKeys.all(wsId), "detail", id] as const,
  children: (wsId: string, id: string) =>
    [...issueKeys.all(wsId), "children", id] as const,
  timeline: (issueId: string) => ["issues", "timeline", issueId] as const,
  reactions: (issueId: string) => ["issues", "reactions", issueId] as const,
  subscribers: (issueId: string) =>
    ["issues", "subscribers", issueId] as const,
  usage: (issueId: string) => ["issues", "usage", issueId] as const,
  taskFileTree: (issueId: string, taskId: string) =>
    ["issues", "taskFileTree", issueId, taskId] as const,
  taskFileContent: (issueId: string, taskId: string, filePath: string) =>
    ["issues", "taskFileContent", issueId, taskId, filePath] as const,
  taskFileDiff: (issueId: string, taskId: string, filePath: string) =>
    ["issues", "taskFileDiff", issueId, taskId, filePath] as const,
};

export type MyIssuesFilter = Pick<ListIssuesParams, "assignee_id" | "assignee_ids" | "creator_id">;

export const CLOSED_PAGE_SIZE = 50;

/**
 * CACHE SHAPE NOTE: The raw cache stores ListIssuesResponse ({ issues, total, doneTotal }),
 * but `select` transforms it to Issue[] for consumers. Mutations and ws-updaters
 * must use setQueryData<ListIssuesResponse>(...) — NOT setQueryData<Issue[]>.
 *
 * Fetches all open issues + first page of done issues. Use useLoadMoreDoneIssues()
 * to paginate additional done items into the cache.
 */
export function issueListOptions(wsId: string) {
  return queryOptions({
    queryKey: issueKeys.list(wsId),
    queryFn: async () => {
      const [openRes, closedRes] = await Promise.all([
        api.listIssues({ open_only: true }),
        api.listIssues({ status: "done", limit: CLOSED_PAGE_SIZE, offset: 0 }),
      ]);
      return {
        issues: [...openRes.issues, ...closedRes.issues],
        total: openRes.total + closedRes.total,
        doneTotal: closedRes.total,
      };
    },
    select: (data) => data.issues,
  });
}

/**
 * Server-filtered issue list for the My Issues page.
 * Each scope gets its own cache entry so switching tabs is instant after first load.
 */
export function myIssueListOptions(
  wsId: string,
  scope: string,
  filter: MyIssuesFilter,
) {
  return queryOptions({
    queryKey: issueKeys.myList(wsId, scope, filter),
    queryFn: async () => {
      const [openRes, closedRes] = await Promise.all([
        api.listIssues({ open_only: true, ...filter }),
        api.listIssues({
          status: "done",
          limit: CLOSED_PAGE_SIZE,
          offset: 0,
          ...filter,
        }),
      ]);
      return {
        issues: [...openRes.issues, ...closedRes.issues],
        total: openRes.total + closedRes.total,
        doneTotal: closedRes.total,
      };
    },
    select: (data) => data.issues,
  });
}

export function issueDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: issueKeys.detail(wsId, id),
    queryFn: () => api.getIssue(id),
  });
}

export function childIssuesOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: issueKeys.children(wsId, id),
    queryFn: () => api.listChildIssues(id).then((r) => r.issues),
  });
}

export function issueTimelineOptions(issueId: string) {
  return queryOptions({
    queryKey: issueKeys.timeline(issueId),
    queryFn: () => api.listTimeline(issueId),
  });
}

export function issueReactionsOptions(issueId: string) {
  return queryOptions({
    queryKey: issueKeys.reactions(issueId),
    queryFn: async () => {
      const issue = await api.getIssue(issueId);
      return issue.reactions ?? [];
    },
  });
}

export function issueSubscribersOptions(issueId: string) {
  return queryOptions({
    queryKey: issueKeys.subscribers(issueId),
    queryFn: () => api.listIssueSubscribers(issueId),
  });
}

export function issueUsageOptions(issueId: string) {
  return queryOptions({
    queryKey: issueKeys.usage(issueId),
    queryFn: () => api.getIssueUsage(issueId),
  });
}

export interface TaskFileTreeData {
  tree: FileNode[];
  git_status: Record<string, GitFileStatus>;
}

/**
 * Task file tree query — initially fetched via REST on mount, then kept fresh
 * by WS `task:file_tree` events injected into the cache by the event handler.
 */
export function taskFileTreeOptions(issueId: string, taskId: string) {
  return queryOptions({
    queryKey: issueKeys.taskFileTree(issueId, taskId),
    queryFn: async () => {
      const data = await api.getTaskFileTree(issueId, taskId);
      return data as TaskFileTreeData;
    },
    enabled: !!issueId && !!taskId,
    // Don't refetch automatically — WS events keep it current for active tasks.
    staleTime: Infinity,
    retry: false,
  });
}

export function taskFileContentOptions(
  issueId: string,
  taskId: string,
  filePath: string,
) {
  return queryOptions({
    queryKey: issueKeys.taskFileContent(issueId, taskId, filePath),
    queryFn: () => api.getTaskFileContent(issueId, taskId, filePath),
    enabled: !!filePath,
    staleTime: 10_000,
  });
}

export function taskFileDiffOptions(
  issueId: string,
  taskId: string,
  filePath: string,
) {
  return queryOptions({
    queryKey: issueKeys.taskFileDiff(issueId, taskId, filePath),
    queryFn: () => api.getTaskFileDiff(issueId, taskId, filePath),
    enabled: !!filePath,
    staleTime: 10_000,
  });
}
