"use client";

import { useQueries, useQuery } from "@tanstack/react-query";
import {
  childIssuesOptions,
  childIssueProgressOptions,
  issueAttachmentsOptions,
  issueDetailOptions,
  issueListOptions,
  issueReactionsOptions,
  issueTaskRunsOptions,
  issueKeys,
  taskMessagesOptions,
} from "./queries";
import { api } from "../api";
import type { TimelineEntry } from "../types";
export { useLiveIssueTasks, type LiveIssueTask } from "./live-tasks";

export function useIssueList(workspaceId: string) {
  return useQuery(issueListOptions(workspaceId));
}

export function useIssueDetail(workspaceId: string, issueId: string) {
  return useQuery(issueDetailOptions(workspaceId, issueId));
}

export function useOptionalIssueDetail(workspaceId: string, issueId: string | null | undefined) {
  return useQuery({
    ...issueDetailOptions(workspaceId, issueId ?? ""),
    enabled: Boolean(issueId),
  });
}

export function useChildIssues(workspaceId: string, issueId: string) {
  return useQuery(childIssuesOptions(workspaceId, issueId));
}

export function useChildIssueProgress(workspaceId: string) {
  return useQuery(childIssueProgressOptions(workspaceId));
}

export function useIssueReactions(_workspaceId: string, issueId: string) {
  return useQuery(issueReactionsOptions(issueId));
}

export function useIssueAttachments(_workspaceId: string, issueId: string) {
  return useQuery(issueAttachmentsOptions(issueId));
}

export function useIssueTaskRuns(_workspaceId: string, issueId: string) {
  return useQuery(issueTaskRunsOptions(issueId));
}

export function useTaskMessages(_workspaceId: string, taskId: string) {
  return useQuery(taskMessagesOptions(taskId));
}

export function useTaskMessagesQueries(_workspaceId: string, taskIds: string[]) {
  return useQueries({
    queries: taskIds.map((taskId) => taskMessagesOptions(taskId)),
  });
}


export function useIssueTimelineEntries(_workspaceId: string, issueId: string) {
  return useQuery({
    queryKey: issueKeys.timeline(issueId),
    queryFn: async () => {
      const page = await api.listTimeline(issueId);
      return Array.isArray(page.entries) ? page.entries : [];
    },
    select: (entries): TimelineEntry[] => (Array.isArray(entries) ? entries : []),
    enabled: !!issueId,
  });
}
