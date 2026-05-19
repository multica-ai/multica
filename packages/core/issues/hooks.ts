"use client";

import { useCallback } from "react";
import { useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  childIssuesOptions,
  childIssueProgressOptions,
  issueAttachmentsOptions,
  issueDetailOptions,
  issueListOptions,
  issueReactionsOptions,
  issueSubscribersOptions,
  issueTaskRunsOptions,
  issueKeys,
  taskMessagesOptions,
} from "./queries";
import { useToggleIssueSubscriber } from "./mutations";
import { api } from "../api";
import { useWSEvent, useWSReconnect } from "../realtime";
import type {
  IssueSubscriber,
  SubscriberAddedPayload,
  SubscriberRemovedPayload,
  TimelineEntry,
} from "../types";
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

export function useIssueSubscribers(_workspaceId: string, issueId: string, userId?: string) {
  const qc = useQueryClient();
  const { data: subscribers = [], isLoading: loading } = useQuery(
    issueSubscribersOptions(issueId),
  );
  const toggleMutation = useToggleIssueSubscriber(issueId);

  useWSReconnect(
    useCallback(() => {
      qc.invalidateQueries({ queryKey: issueKeys.subscribers(issueId) });
    }, [qc, issueId]),
  );

  useWSEvent(
    "subscriber:added",
    useCallback(
      (payload: unknown) => {
        const p = payload as SubscriberAddedPayload;
        if (p.issue_id !== issueId) return;
        qc.setQueryData<IssueSubscriber[]>(
          issueKeys.subscribers(issueId),
          (old) => {
            if (!old) return old;
            if (
              old.some(
                (s) =>
                  s.user_id === p.user_id && s.user_type === p.user_type,
              )
            )
              return old;
            return [
              ...old,
              {
                issue_id: p.issue_id,
                user_type: p.user_type as "member" | "agent",
                user_id: p.user_id,
                reason: p.reason as IssueSubscriber["reason"],
                created_at: new Date().toISOString(),
              },
            ];
          },
        );
      },
      [qc, issueId],
    ),
  );

  useWSEvent(
    "subscriber:removed",
    useCallback(
      (payload: unknown) => {
        const p = payload as SubscriberRemovedPayload;
        if (p.issue_id !== issueId) return;
        qc.setQueryData<IssueSubscriber[]>(
          issueKeys.subscribers(issueId),
          (old) =>
            old?.filter(
              (s) =>
                !(s.user_id === p.user_id && s.user_type === p.user_type),
            ),
        );
      },
      [qc, issueId],
    ),
  );

  const isSubscribed = subscribers.some(
    (s) => s.user_type === "member" && s.user_id === userId,
  );

  const toggleSubscriber = useCallback(
    (
      subUserId: string,
      userType: "member" | "agent",
      currentlySubscribed: boolean,
    ) => {
      toggleMutation.mutate({
        userId: subUserId,
        userType,
        subscribed: currentlySubscribed,
      });
    },
    [toggleMutation],
  );

  const toggleSubscribe = useCallback(() => {
    if (userId) toggleSubscriber(userId, "member", isSubscribed);
  }, [userId, isSubscribed, toggleSubscriber]);

  return {
    subscribers,
    loading,
    isSubscribed,
    isToggling: toggleMutation.isPending,
    toggleSubscribe,
    toggleSubscriber,
  };
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
    queryFn: () => api.listTimeline(issueId),
    select: (entries): TimelineEntry[] => (Array.isArray(entries) ? entries : []),
    enabled: !!issueId,
  });
}
