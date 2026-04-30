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
  issueTimelineOptions,
  taskMessagesOptions,
} from "./queries";

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

export function useIssueTimelineEntries(workspaceId: string, issueId: string) {
  return useQuery(issueTimelineOptions(workspaceId, issueId));
}

export function useChildIssues(workspaceId: string, issueId: string) {
  return useQuery(childIssuesOptions(workspaceId, issueId));
}

export function useChildIssueProgress(workspaceId: string) {
  return useQuery(childIssueProgressOptions(workspaceId));
}

export function useIssueReactions(workspaceId: string, issueId: string) {
  return useQuery(issueReactionsOptions(workspaceId, issueId));
}

export function useIssueAttachments(workspaceId: string, issueId: string) {
  return useQuery(issueAttachmentsOptions(workspaceId, issueId));
}

export function useIssueTaskRuns(workspaceId: string, issueId: string) {
  return useQuery(issueTaskRunsOptions(workspaceId, issueId));
}

export function useTaskMessages(workspaceId: string, taskId: string) {
  return useQuery(taskMessagesOptions(workspaceId, taskId));
}

export function useTaskMessagesQueries(workspaceId: string, taskIds: string[]) {
  return useQueries({
    queries: taskIds.map((taskId) => taskMessagesOptions(workspaceId, taskId)),
  });
}
