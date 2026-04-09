"use client";

import { useState, useEffect, useCallback } from "react";
import type { IssueReaction } from "@/shared/types";
import type {
  IssueReactionAddedPayload,
  IssueReactionRemovedPayload,
} from "@/shared/types";
import { api } from "@/shared/api";
import { toast } from "sonner";
import { useWSEvent, useWSReconnect } from "@/features/realtime";

export function useIssueReactions(issueId: string, userId?: string) {
  const [reactions, setReactions] = useState<IssueReaction[]>([]);
  const [loading, setLoading] = useState(true);

  // Initial fetch
  useEffect(() => {
    setReactions([]);
    setLoading(true);
    api
      .getIssue(issueId)
      .then((iss) => setReactions(iss.reactions ?? []))
      .catch((e) => {
        console.error(e);
        toast.error("Failed to load reactions");
      })
      .finally(() => setLoading(false));
  }, [issueId]);

  // Reconnect recovery
  useWSReconnect(
    useCallback(() => {
      api.getIssue(issueId).then((iss) => setReactions(iss.reactions ?? [])).catch(console.error);
    }, [issueId]),
  );

  // --- WS event handlers ---

  useWSEvent(
    "issue_reaction:added",
    useCallback(
      (payload: unknown) => {
        const { reaction, issue_id } = payload as IssueReactionAddedPayload;
        if (issue_id !== issueId) return;
        if (reaction.actor_type === "member" && reaction.actor_id === userId) return;
        setReactions((prev) => {
          if (prev.some((r) => r.id === reaction.id)) return prev;
          return [...prev, reaction];
        });
      },
      [issueId, userId],
    ),
  );

  useWSEvent(
    "issue_reaction:removed",
    useCallback(
      (payload: unknown) => {
        const p = payload as IssueReactionRemovedPayload;
        if (p.issue_id !== issueId) return;
        if (p.actor_type === "member" && p.actor_id === userId) return;
        setReactions((prev) =>
          prev.filter(
            (r) => !(r.emoji === p.emoji && r.actor_type === p.actor_type && r.actor_id === p.actor_id),
          ),
        );
      },
      [issueId, userId],
    ),
  );

  // --- Mutation ---

  const toggleReaction = useCallback(
    async (emoji: string) => {
      if (!userId) return;
      const existing = reactions.find(
        (r) => r.emoji === emoji && r.actor_type === "member" && r.actor_id === userId,
      );
      if (existing) {
        setReactions((prev) => prev.filter((r) => r.id !== existing.id));
        try {
          await api.removeIssueReaction(issueId, emoji);
        } catch {
          setReactions((prev) => [...prev, existing]);
          toast.error("Failed to remove reaction");
        }
      } else {
        const temp: IssueReaction = {
          id: `temp-${Date.now()}`,
          issue_id: issueId,
          actor_type: "member",
          actor_id: userId,
          emoji,
          created_at: new Date().toISOString(),
        };
        setReactions((prev) => [...prev, temp]);
        try {
          const reaction = await api.addIssueReaction(issueId, emoji);
          setReactions((prev) => prev.map((r) => (r.id === temp.id ? reaction : r)));
        } catch {
          setReactions((prev) => prev.filter((r) => r.id !== temp.id));
          toast.error("Failed to add reaction");
        }
      }
    },
    [issueId, userId, reactions],
  );

  return { reactions, loading, toggleReaction };
}
