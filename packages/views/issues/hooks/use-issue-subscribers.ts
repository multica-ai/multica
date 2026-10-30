"use client";

import { useCallback, useRef } from "react";
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
  const { data: subscribers = [], isSuccess } = useQuery(
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
  // Both `subscribers` and `isSubscribed` come from `data ?? []`, so before the
  // query resolves they read "nobody is subscribed" — including for people who
  // are. EVERY control derived from them must gate on this rather than render
  // the default, and that means the subscriber picker too, not just the
  // subscribe button: an unchecked row for someone already subscribed sends an
  // explicit subscribe, which rewrites their reason to 'manual' and clears any
  // opt-out scope (server/pkg/db/queries/subscriber.sql). A failed query stays
  // unknown as well; only a resolved one is truth (MUL-5714).
  const subscriptionKnown = isSuccess;
  // Why the current user is watching. Drives the "your agent created this on
  // your behalf" explanation — a subscription nobody remembers opting into
  // reads as the product being creepy unless it says why (MUL-5483).
  const subscriptionReason = ownSubscription?.reason;

  // Serializes direct toggles. Disabling the button covers the ordinary case,
  // but React Query flushes `isPending` in a microtask, so two clicks in the
  // same tick both reach a still-enabled control. Overlapping toggles are the
  // one thing useToggleIssueSubscriber's whole-list optimistic snapshot cannot
  // survive: the second call snapshots the first one's patch and, on failure,
  // rolls back to it instead of to the server's state (MUL-5714).
  const toggleInFlight = useRef(false);

  // The optimistic patch in useToggleIssueSubscriber rolls itself back on
  // failure, which puts the row back exactly as it was — indistinguishable from
  // a button that never fired. Say so instead (MUL-5714).
  const toggleSubscriber = useCallback(
    (
      subUserId: string,
      userType: "member" | "agent",
      currentlySubscribed: boolean,
    ) => {
      if (toggleInFlight.current) return;
      toggleInFlight.current = true;
      toggleMutation.mutate(
        {
          userId: subUserId,
          userType,
          subscribed: currentlySubscribed,
        },
        {
          onError: () =>
            toast.error(t(($) => $.detail.subscription_update_failed)),
          // Runs after the mutation's own onSettled, so the cache has already
          // been invalidated by the time the next toggle can start.
          onSettled: () => {
            toggleInFlight.current = false;
          },
        },
      );
    },
    [toggleMutation, t],
  );

  const toggleSubscribe = useCallback(() => {
    if (userId) toggleSubscriber(userId, "member", isSubscribed);
  }, [userId, isSubscribed, toggleSubscriber]);

  // Subscription state is server-owned and the row simply does not change on
  // failure, so nothing on screen moves. Without an explicit message the user
  // reads a failed unsubscribe as a no-op button and tries again forever.
  //
  // Success needs a message for the same reason, and only here: this mutation
  // is deliberately not optimistic (it retires an unknown number of descendant
  // subscriptions), so unlike the direct toggle there is no label or avatar
  // flipping to confirm the click landed (MUL-5714).
  const unsubscribeFromSubtree = useCallback(() => {
    if (!userId) return;
    subtreeMutation.mutate(
      { userId, userType: "member" },
      {
        onSuccess: () =>
          toast.success(t(($) => $.detail.unsubscribe_subtree_succeeded)),
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
    subscriptionKnown,
    isSubscribed,
    subscriptionReason,
    // Serializing the UI on these is what keeps the whole-list optimistic
    // snapshot in useToggleIssueSubscriber safe: concurrent toggles would each
    // snapshot the other's in-flight patch and roll back to the wrong list.
    togglePending: toggleMutation.isPending,
    subtreePending: subtreeMutation.isPending,
    toggleSubscribe,
    toggleSubscriber,
    unsubscribeFromSubtree,
  };
}
