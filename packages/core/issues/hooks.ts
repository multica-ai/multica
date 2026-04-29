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

export function useIssueTimelineEntries(issueId: string) {
  return useQuery(issueTimelineOptions(issueId));
}

export function useChildIssues(workspaceId: string, issueId: string) {
  return useQuery(childIssuesOptions(workspaceId, issueId));
}

export function useChildIssueProgress(workspaceId: string) {
  return useQuery(childIssueProgressOptions(workspaceId));
}

export function useIssueReactions(issueId: string) {
  return useQuery(issueReactionsOptions(issueId));
}

export function useIssueAttachments(issueId: string) {
  return useQuery(issueAttachmentsOptions(issueId));
}

export function useIssueTaskRuns(issueId: string) {
  return useQuery(issueTaskRunsOptions(issueId));
}

export function useTaskMessages(taskId: string) {
  return useQuery(taskMessagesOptions(taskId));
}

export function useTaskMessagesQueries(taskIds: string[]) {
  return useQueries({
    queries: taskIds.map((taskId) => taskMessagesOptions(taskId)),
  });
}
