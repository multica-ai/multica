"use client";

import { useCallback, useMemo } from "react";
import { useQuery, useQueryClient, useMutationState } from "@tanstack/react-query";
import type { IssueReaction } from "@multica/core/types";
import type {
  IssueReactionAddedPayload,
  IssueReactionRemovedPayload,
} from "@multica/core/types";
import { issueReactionsOptions, issueKeys } from "@multica/core/issues/queries";
import { useToggleIssueReaction, type ToggleIssueReactionVars } from "@multica/core/issues/mutations";
import { useWSEvent, useWSReconnect } from "@multica/core/realtime";

export function useIssueReactions(issueId: string, userId?: string) {
  const qc = useQueryClient();
  const { data: serverReactions = [], isLoading: loading } = useQuery(
    issueReactionsOptions(issueId),
  );

  const toggleMutation = useToggleIssueReaction(issueId);

  // Reconnect recovery
  useWSReconnect(
    useCallback(() => {
      qc.invalidateQueries({ queryKey: issueKeys.reactions(issueId) });
    }, [qc, issueId]),
  );

  // --- WS event handlers (update server cache for other users' actions) ---

  useWSEvent(
    "issue_reaction:added",
    useCallback(
      (payload: unknown) => {
        const { reaction, issue_id } = payload as IssueReactionAddedPayload;
        if (issue_id !== issueId) return;
        qc.setQueryData<IssueReaction[]>(
          issueKeys.reactions(issueId),
          (old) => {
            if (!old) return old;
            if (old.some((r) => r.id === reaction.id)) return old;
            return [...old, reaction];
          },
        );
      },
      [qc, issueId],
    ),
  );

  useWSEvent(
    "issue_reaction:removed",
    useCallback(
      (payload: unknown) => {
        const p = payload as IssueReactionRemovedPayload;
        if (p.issue_id !== issueId) return;
        qc.setQueryData<IssueReaction[]>(
          issueKeys.reactions(issueId),
          (old) =>
            old?.filter(
              (r) =>
                !(
                  r.emoji === p.emoji &&
                  r.actor_type === p.actor_type &&
                  r.actor_id === p.actor_id
                ),
            ),
        );
      },
      [qc, issueId],
    ),
  );

  // --- Optimistic UI derivation ---
  // Instead of writing temp data into the cache (which races with WS events),
  // derive optimistic state at render time from pending mutation variables.

  const pendingVars = useMutationState({
    filters: {
      mutationKey: ["toggleIssueReaction", issueId],
      status: "pending",
    },
    select: (m) =>
      m.state.variables as ToggleIssueReactionVars | undefined,
  });

  const reactions = useMemo(() => {
    if (pendingVars.length === 0) return serverReactions;

    let result = [...serverReactions];
    for (const vars of pendingVars) {
      if (!vars) continue;
      if (vars.existing) {
        // Pending removal
        result = result.filter((r) => r.id !== vars.existing!.id);
      } else {
        // Pending add — skip if server already has it (WS arrived first)
        const alreadyExists = result.some(
          (r) =>
            r.emoji === vars.emoji &&
            r.actor_type === "member" &&
            r.actor_id === userId,
        );
        if (!alreadyExists) {
          result = [
            ...result,
            {
              id: `optimistic-${vars.emoji}`,
              issue_id: issueId,
              actor_type: "member",
              actor_id: userId ?? "",
              emoji: vars.emoji,
              created_at: "",
            },
          ];
        }
      }
    }
    return result;
  }, [serverReactions, pendingVars, issueId, userId]);

  // --- Mutation ---

  const toggleReaction = useCallback(
    async (emoji: string) => {
      if (!userId) return;
      const existing = serverReactions.find(
        (r) =>
          r.emoji === emoji &&
          r.actor_type === "member" &&
          r.actor_id === userId,
      );
      toggleMutation.mutate({ emoji, existing });
    },
    [userId, serverReactions, toggleMutation],
  );

  return { reactions, loading, toggleReaction };
}
