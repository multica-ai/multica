"use client";

import { useCallback, useMemo } from "react";
import { useQuery, useMutationState } from "@tanstack/react-query";
import type { Issue, IssueReaction } from "@multica/core/types";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useToggleIssueReaction, type ToggleIssueReactionVars } from "@multica/core/issues/mutations";
import { useWorkspaceId } from "@multica/core/hooks";

const NO_REACTIONS: IssueReaction[] = [];

const selectReactions = (issue: Issue) => issue.reactions ?? NO_REACTIONS;

/**
 * Reactions are read off the issue detail query, so opening an issue costs a
 * single `GET /api/issues/:id`. Realtime events and the toggle mutation patch
 * that same cache entry (`patchIssueReactions`), and reconnect recovery
 * refetches it with the rest of the workspace's issue caches.
 */
export function useIssueReactions(issueId: string, userId?: string) {
  const wsId = useWorkspaceId();
  const { data: serverReactions = NO_REACTIONS, isLoading: loading } = useQuery({
    ...issueDetailOptions(wsId, issueId),
    select: selectReactions,
  });

  const toggleMutation = useToggleIssueReaction(issueId);

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
