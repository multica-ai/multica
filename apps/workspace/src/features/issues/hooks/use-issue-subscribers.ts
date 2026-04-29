"use client";

import { useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { IssueSubscriber } from "@/shared/types";
import type {
  SubscriberAddedPayload,
  SubscriberRemovedPayload,
} from "@/shared/types";
import { toast } from "sonner";
import { useWSEvent, useWSReconnect } from "@/features/realtime";
import { queryKeys } from "@/shared/query";
import { useIssueSubscribersMutations } from "../mutations";
import { useIssueSubscribersQuery } from "../queries";

export function useIssueSubscribers(issueId: string, userId?: string) {
  const queryClient = useQueryClient();
  const subscribersQuery = useIssueSubscribersQuery(issueId);
  const { toggleSubscriber } = useIssueSubscribersMutations(issueId);
  const subscribers = subscribersQuery.data ?? [];
  const loading = subscribersQuery.isPending;

  // Reconnect recovery
  useWSReconnect(
    useCallback(() => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.issues.subscribers(issueId) });
    }, [issueId, queryClient]),
  );

  // --- WS event handlers ---

  useWSEvent(
    "subscriber:added",
    useCallback(
      (payload: unknown) => {
        const p = payload as SubscriberAddedPayload;
        if (p.issue_id !== issueId) return;
        queryClient.setQueryData<IssueSubscriber[]>(queryKeys.issues.subscribers(issueId), (existing = []) => {
          if (existing.some((subscriber) => subscriber.user_id === p.user_id && subscriber.user_type === p.user_type)) {
            return existing;
          }
          return [
            ...existing,
            {
              issue_id: p.issue_id,
              user_type: p.user_type as "member" | "agent",
              user_id: p.user_id,
              reason: p.reason as IssueSubscriber["reason"],
              created_at: new Date().toISOString(),
            },
          ];
        });
      },
      [issueId, queryClient],
    ),
  );

  useWSEvent(
    "subscriber:removed",
    useCallback(
      (payload: unknown) => {
        const p = payload as SubscriberRemovedPayload;
        if (p.issue_id !== issueId) return;
        queryClient.setQueryData<IssueSubscriber[]>(queryKeys.issues.subscribers(issueId), (existing = []) =>
          existing.filter((subscriber) => !(subscriber.user_id === p.user_id && subscriber.user_type === p.user_type)),
        );
      },
      [issueId, queryClient],
    ),
  );

  const isSubscribed = subscribers.some(
    (s) => s.user_type === "member" && s.user_id === userId,
  );

  const toggleSubscriberSafe = useCallback(
    async (subUserId: string, userType: "member" | "agent", currentlySubscribed: boolean) => {
      try {
        await toggleSubscriber(subUserId, userType, !currentlySubscribed);
      } catch {
        toast.error("Failed to update subscriber");
      }
    },
    [toggleSubscriber],
  );

  const toggleSubscribe = useCallback(() => {
    if (userId) {
      void toggleSubscriberSafe(userId, "member", isSubscribed);
    }
  }, [userId, isSubscribed, toggleSubscriberSafe]);

  return {
    subscribers,
    loading,
    isSubscribed,
    toggleSubscribe,
    toggleSubscriber: toggleSubscriberSafe,
  };
}
