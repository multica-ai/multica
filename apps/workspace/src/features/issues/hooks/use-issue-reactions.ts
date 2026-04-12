"use client";

import { useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { IssueReaction } from "@/shared/types";
import type {
  IssueReactionAddedPayload,
  IssueReactionRemovedPayload,
} from "@/shared/types";
import { toast } from "sonner";
import { useWSEvent, useWSReconnect } from "@/features/realtime";
import { queryKeys } from "@/shared/query";
import { useIssueReactionMutations } from "../mutations";
import { useIssueReactionsQuery } from "../queries";

export function useIssueReactions(issueId: string, userId?: string) {
  const queryClient = useQueryClient();
  const reactionsQuery = useIssueReactionsQuery(issueId);
  const { toggleIssueReaction } = useIssueReactionMutations(issueId);
  const reactions = reactionsQuery.data ?? [];
  const loading = reactionsQuery.isPending;

  // Reconnect recovery
  useWSReconnect(
    useCallback(() => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.issues.reactions(issueId) });
    }, [issueId, queryClient]),
  );

  // --- WS event handlers ---

  useWSEvent(
    "issue_reaction:added",
    useCallback(
      (payload: unknown) => {
        const { reaction, issue_id } = payload as IssueReactionAddedPayload;
        if (issue_id !== issueId) return;
        if (reaction.actor_type === "member" && reaction.actor_id === userId) return;
        queryClient.setQueryData<IssueReaction[]>(queryKeys.issues.reactions(issueId), (existing = []) => {
          if (existing.some((item) => item.id === reaction.id)) return existing;
          return [...existing, reaction];
        });
      },
      [issueId, queryClient, userId],
    ),
  );

  useWSEvent(
    "issue_reaction:removed",
    useCallback(
      (payload: unknown) => {
        const p = payload as IssueReactionRemovedPayload;
        if (p.issue_id !== issueId) return;
        if (p.actor_type === "member" && p.actor_id === userId) return;
        queryClient.setQueryData<IssueReaction[]>(queryKeys.issues.reactions(issueId), (existing = []) =>
          existing.filter(
            (reaction) =>
              !(reaction.emoji === p.emoji && reaction.actor_type === p.actor_type && reaction.actor_id === p.actor_id),
          ),
        );
      },
      [issueId, queryClient, userId],
    ),
  );

  const toggleReaction = useCallback(
    async (emoji: string) => {
      if (!userId) return;
      const existing = reactions.find(
        (r) => r.emoji === emoji && r.actor_type === "member" && r.actor_id === userId,
      );
      try {
        await toggleIssueReaction(emoji, existing?.id);
      } catch {
        toast.error(existing ? "Failed to remove reaction" : "Failed to add reaction");
      }
    },
    [reactions, toggleIssueReaction, userId],
  );

  return { reactions, loading, toggleReaction };
}
