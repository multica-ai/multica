import { useQuery } from "@tanstack/react-query";
import type {
  AgentTask,
  Issue,
  IssueReaction,
  IssueSubscriber,
  ListIssuesParams,
  TaskMessagePayload,
  TimelineEntry,
} from "@/shared/types";
import { api } from "@/shared/api";
import { queryKeys } from "@/shared/query";
import { hasStoredSessionToken } from "@/features/auth/queries";
import { useWorkspaceStore } from "@/features/workspace";

const ISSUES_STALE_TIME = 30 * 1000;
const ISSUE_LIST_LIMIT = 200;

export function issuesListQueryOptions(
  workspaceId: string,
  params: ListIssuesParams = {},
) {
  return {
    queryKey: queryKeys.issues.list(workspaceId, { limit: ISSUE_LIST_LIMIT, ...params }),
    queryFn: () => api.listIssues({ limit: ISSUE_LIST_LIMIT, workspace_id: workspaceId, ...params }),
    staleTime: ISSUES_STALE_TIME,
  };
}

export function issueDetailQueryOptions(issueId: string) {
  return {
    queryKey: queryKeys.issues.detail(issueId),
    queryFn: () => api.getIssue(issueId),
    staleTime: ISSUES_STALE_TIME,
  };
}

export function issueTimelineQueryOptions(issueId: string) {
  return {
    queryKey: queryKeys.issues.timeline(issueId),
    queryFn: () => api.listTimeline(issueId),
    staleTime: ISSUES_STALE_TIME,
  };
}

export function issueSubscribersQueryOptions(issueId: string) {
  return {
    queryKey: queryKeys.issues.subscribers(issueId),
    queryFn: () => api.listIssueSubscribers(issueId),
    staleTime: ISSUES_STALE_TIME,
  };
}

export function issueReactionsQueryOptions(issueId: string) {
  return {
    queryKey: queryKeys.issues.reactions(issueId),
    queryFn: async () => {
      const issue = await api.getIssue(issueId);
      return issue.reactions ?? [];
    },
    staleTime: ISSUES_STALE_TIME,
  };
}

export function issueActiveTaskQueryOptions(issueId: string) {
  return {
    queryKey: queryKeys.tasks.activeByIssue(issueId),
    queryFn: async () => {
      const response = await api.getActiveTaskForIssue(issueId);
      return response.task;
    },
    staleTime: ISSUES_STALE_TIME,
  };
}

export function issueTaskRunsQueryOptions(issueId: string) {
  return {
    queryKey: queryKeys.tasks.byIssue(issueId),
    queryFn: () => api.listTasksByIssue(issueId),
    staleTime: ISSUES_STALE_TIME,
  };
}

export function taskMessagesQueryOptions(taskId: string) {
  return {
    queryKey: queryKeys.tasks.messages(taskId),
    queryFn: () => api.listTaskMessages(taskId),
    staleTime: ISSUES_STALE_TIME,
  };
}

export function useIssuesListQuery(params: ListIssuesParams = {}) {
  const workspaceId = useWorkspaceStore((state) => state.workspace?.id);

  return useQuery<{ issues: Issue[]; total: number }>({
    ...(workspaceId
      ? issuesListQueryOptions(workspaceId, params)
      : {
          queryKey: queryKeys.issues.list("__no_workspace__", { limit: ISSUE_LIST_LIMIT, ...params }),
          queryFn: async () => ({ issues: [] as Issue[], total: 0 }),
          staleTime: ISSUES_STALE_TIME,
        }),
    enabled: Boolean(workspaceId) && hasStoredSessionToken(),
  });
}

export function useIssueDetailQuery(issueId?: string | null) {
  return useQuery<Issue | null>({
    ...(issueId
      ? issueDetailQueryOptions(issueId)
      : {
          queryKey: queryKeys.issues.detail("__missing_issue__"),
          queryFn: async () => null,
          staleTime: ISSUES_STALE_TIME,
        }),
    enabled: Boolean(issueId) && hasStoredSessionToken(),
  });
}

export function useIssueTimelineQuery(issueId?: string | null) {
  return useQuery<TimelineEntry[]>({
    ...(issueId
      ? issueTimelineQueryOptions(issueId)
      : {
          queryKey: queryKeys.issues.timeline("__missing_issue__"),
          queryFn: async () => [] as TimelineEntry[],
          staleTime: ISSUES_STALE_TIME,
        }),
    enabled: Boolean(issueId) && hasStoredSessionToken(),
  });
}

export function useIssueSubscribersQuery(issueId?: string | null) {
  return useQuery<IssueSubscriber[]>({
    ...(issueId
      ? issueSubscribersQueryOptions(issueId)
      : {
          queryKey: queryKeys.issues.subscribers("__missing_issue__"),
          queryFn: async () => [] as IssueSubscriber[],
          staleTime: ISSUES_STALE_TIME,
        }),
    enabled: Boolean(issueId) && hasStoredSessionToken(),
  });
}

export function useIssueReactionsQuery(issueId?: string | null) {
  return useQuery<IssueReaction[]>({
    ...(issueId
      ? issueReactionsQueryOptions(issueId)
      : {
          queryKey: queryKeys.issues.reactions("__missing_issue__"),
          queryFn: async () => [] as IssueReaction[],
          staleTime: ISSUES_STALE_TIME,
        }),
    enabled: Boolean(issueId) && hasStoredSessionToken(),
  });
}

export function useIssueActiveTaskQuery(issueId?: string | null) {
  return useQuery<AgentTask | null>({
    ...(issueId
      ? issueActiveTaskQueryOptions(issueId)
      : {
          queryKey: queryKeys.tasks.activeByIssue("__missing_issue__"),
          queryFn: async () => null,
          staleTime: ISSUES_STALE_TIME,
        }),
    enabled: Boolean(issueId) && hasStoredSessionToken(),
  });
}

export function useIssueTaskRunsQuery(issueId?: string | null) {
  return useQuery<AgentTask[]>({
    ...(issueId
      ? issueTaskRunsQueryOptions(issueId)
      : {
          queryKey: queryKeys.tasks.byIssue("__missing_issue__"),
          queryFn: async () => [] as AgentTask[],
          staleTime: ISSUES_STALE_TIME,
        }),
    enabled: Boolean(issueId) && hasStoredSessionToken(),
  });
}

export function useTaskMessagesQuery(taskId?: string | null) {
  return useQuery<TaskMessagePayload[]>({
    ...(taskId
      ? taskMessagesQueryOptions(taskId)
      : {
          queryKey: queryKeys.tasks.messages("__missing_task__"),
          queryFn: async () => [] as TaskMessagePayload[],
          staleTime: ISSUES_STALE_TIME,
        }),
    enabled: Boolean(taskId) && hasStoredSessionToken(),
  });
}
