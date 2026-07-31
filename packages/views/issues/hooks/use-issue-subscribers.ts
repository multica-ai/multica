"use client";

import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { IssueSubscriber } from "@multica/core/types";
import type {
  SubscriberAddedPayload,
  SubscriberRemovedPayload,
} from "@multica/core/types";
import { issueSubscribersOptions, issueKeys } from "@multica/core/issues/queries";
import {
  useToggleIssueSubscriber,
  useUnsubscribeFromIssueSubtree,
} from "@multica/core/issues/mutations";
import { useWSEvent, useWSReconnect } from "@multica/core/realtime";
// Imported from the defining module, not the "@multica/core/api" barrel: the
// barrel drags the client singleton and ws-client into the module graph of
// every consumer of this hook, which measurably slowed test startup and tipped
// an unrelated timing-sensitive suite over its waitFor budget.
import { ApiError } from "@multica/core/api/client";
import { toast } from "sonner";
import { useT } from "../../i18n";

/**
 * True only for the deploy-skew 404: a backend that predates
 * /unsubscribe/subtree, where chi answers an unknown ROUTE with plain text.
 *
 * Not every 404 means that. The same endpoint on a current backend returns a
 * structured JSON 404 when the issue is deleted or not visible, and telling
 * that user to "wait for the next update" is wrong advice. ApiError.body is the
 * discriminator: parseErrorBody leaves it undefined when the response was not
 * JSON, and populated when the server sent a real error document
 * (MUL-5483 review round 8).
 */
function isMissingRouteError(err: unknown): boolean {
  return err instanceof ApiError && err.status === 404 && err.body === undefined;
}

export function useIssueSubscribers(issueId: string, userId?: string) {
  const qc = useQueryClient();
  const { t } = useT("issues");
  const { data: subscribers = [], isLoading: loading } = useQuery(
    issueSubscribersOptions(issueId),
  );

  const toggleMutation = useToggleIssueSubscriber(issueId);
  const subtreeMutation = useUnsubscribeFromIssueSubtree(issueId);

  // Reconnect recovery
  useWSReconnect(
    useCallback(() => {
      qc.invalidateQueries({ queryKey: issueKeys.subscribers(issueId) });
    }, [qc, issueId]),
  );

  // --- WS event handlers ---

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
            const existing = old.find(
              (s) => s.user_id === p.user_id && s.user_type === p.user_type,
            );
            // The server re-broadcasts for an existing subscriber only when the
            // reason changed — a delegate who got assigned, mentioned, or
            // commented is upgraded out of the reduced tier. Patch it rather
            // than bailing, or the "Watching via agent" badge keeps claiming a
            // delegation that no longer applies (MUL-5483).
            if (existing) {
              if (existing.reason === p.reason) return old;
              return old.map((s) =>
                s.user_id === p.user_id && s.user_type === p.user_type
                  ? { ...s, reason: p.reason as IssueSubscriber["reason"] }
                  : s,
              );
            }
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

  // --- Mutations ---

  const ownSubscription = subscribers.find(
    (s) => s.user_type === "member" && s.user_id === userId,
  );
  const isSubscribed = !!ownSubscription;
  // Why the current user is watching. Drives the "your agent created this on
  // your behalf" explanation — a subscription nobody remembers opting into
  // reads as the product being creepy unless it says why (MUL-5483).
  const subscriptionReason = ownSubscription?.reason;

  const toggleSubscriber = useCallback(
    async (
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

  // Subscription state is server-owned and the row simply does not change on
  // failure, so nothing on screen moves. Without an explicit message the user
  // reads a failed unsubscribe as a no-op button and tries again forever.
  const unsubscribeFromSubtree = useCallback(() => {
    if (!userId) return;
    subtreeMutation.mutate(
      { userId, userType: "member" },
      {
        onError: (err) =>
          toast.error(
            isMissingRouteError(err)
              ? t(($) => $.detail.unsubscribe_subtree_unsupported)
              : t(($) => $.detail.unsubscribe_subtree_failed),
          ),
      },
    );
  }, [userId, subtreeMutation, t]);

  return {
    subscribers,
    loading,
    isSubscribed,
    subscriptionReason,
    toggleSubscribe,
    toggleSubscriber,
    unsubscribeFromSubtree,
  };
}
