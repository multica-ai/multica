"use client";

import { useState, useEffect, useCallback } from "react";
import type { IssueSubscriber } from "@/shared/types";
import type {
  SubscriberAddedPayload,
  SubscriberRemovedPayload,
} from "@/shared/types";
import { api } from "@/shared/api";
import { toast } from "sonner";
import { useWSEvent, useWSReconnect } from "@/features/realtime";

export function useIssueSubscribers(issueId: string, userId?: string) {
  const [subscribers, setSubscribers] = useState<IssueSubscriber[]>([]);
  const [loading, setLoading] = useState(true);

  // Initial fetch
  useEffect(() => {
    setSubscribers([]);
    setLoading(true);
    api
      .listIssueSubscribers(issueId)
      .then((subs) => setSubscribers(subs))
      .catch((e) => {
        console.error(e);
        toast.error("Failed to load subscribers");
      })
      .finally(() => setLoading(false));
  }, [issueId]);

  // Reconnect recovery
  useWSReconnect(
    useCallback(() => {
      api.listIssueSubscribers(issueId).then(setSubscribers).catch(console.error);
    }, [issueId]),
  );

  // --- WS event handlers ---

  useWSEvent(
    "subscriber:added",
    useCallback(
      (payload: unknown) => {
        const p = payload as SubscriberAddedPayload;
        if (p.issue_id !== issueId) return;
        setSubscribers((prev) => {
          if (prev.some((s) => s.user_id === p.user_id && s.user_type === p.user_type)) return prev;
          return [
            ...prev,
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
      [issueId],
    ),
  );

  useWSEvent(
    "subscriber:removed",
    useCallback(
      (payload: unknown) => {
        const p = payload as SubscriberRemovedPayload;
        if (p.issue_id !== issueId) return;
        setSubscribers((prev) =>
          prev.filter((s) => !(s.user_id === p.user_id && s.user_type === p.user_type)),
        );
      },
      [issueId],
    ),
  );

  // --- Mutations ---

  const isSubscribed = subscribers.some(
    (s) => s.user_type === "member" && s.user_id === userId,
  );

  const toggleSubscriber = useCallback(
    async (subUserId: string, userType: "member" | "agent", currentlySubscribed: boolean) => {
      if (currentlySubscribed) {
        // Optimistic remove + rollback on error
        const removed = subscribers.find(
          (s) => s.user_id === subUserId && s.user_type === userType,
        );
        setSubscribers((prev) =>
          prev.filter((s) => !(s.user_id === subUserId && s.user_type === userType)),
        );
        try {
          await api.unsubscribeFromIssue(issueId, subUserId, userType);
        } catch {
          if (removed) setSubscribers((prev) => [...prev, removed]);
          toast.error("Failed to update subscriber");
        }
      } else {
        // Optimistic add
        const tempSub: IssueSubscriber = {
          issue_id: issueId,
          user_type: userType,
          user_id: subUserId,
          reason: "manual" as const,
          created_at: new Date().toISOString(),
        };
        setSubscribers((prev) => {
          if (prev.some((s) => s.user_id === subUserId && s.user_type === userType)) return prev;
          return [...prev, tempSub];
        });
        try {
          await api.subscribeToIssue(issueId, subUserId, userType);
        } catch {
          setSubscribers((prev) =>
            prev.filter((s) => !(s.user_id === subUserId && s.user_type === userType && s.reason === "manual")),
          );
          toast.error("Failed to update subscriber");
        }
      }
    },
    [issueId, subscribers],
  );

  const toggleSubscribe = useCallback(() => {
    if (userId) toggleSubscriber(userId, "member", isSubscribed);
  }, [userId, isSubscribed, toggleSubscriber]);

  return { subscribers, loading, isSubscribed, toggleSubscribe, toggleSubscriber };
}
