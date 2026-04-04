"use client";

import { useState, useEffect, useCallback } from "react";
import type { Comment, TimelineEntry } from "@/shared/types";
import type {
  CommentCreatedPayload,
  CommentUpdatedPayload,
  CommentDeletedPayload,
  ActivityCreatedPayload,
  ReactionAddedPayload,
  ReactionRemovedPayload,
} from "@/shared/types";
import { api } from "@/shared/api";
import { useWSEvent, useWSReconnect } from "@/features/realtime";
import { toast } from "sonner";

function commentToTimelineEntry(c: Comment): TimelineEntry {
  return {
    type: "comment",
    id: c.id,
    actor_type: c.author_type,
    actor_id: c.author_id,
    content: c.content,
    parent_id: c.parent_id,
    created_at: c.created_at,
    updated_at: c.updated_at,
    comment_type: c.type,
    reactions: c.reactions ?? [],
  };
}

export function useIssueTimeline(issueId: string, userId?: string) {
  const [timeline, setTimeline] = useState<TimelineEntry[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const [loading, setLoading] = useState(true);

  // Initial fetch + reset on id change
  useEffect(() => {
    setTimeline([]);
    setLoading(true);
    api
      .listTimeline(issueId)
      .then((entries) => setTimeline(entries))
      .catch((e) => {
        console.error(e);
        toast.error("Failed to load activity");
      })
      .finally(() => setLoading(false));
  }, [issueId]);

  // Reconnect recovery
  useWSReconnect(
    useCallback(() => {
      api.listTimeline(issueId).then(setTimeline).catch(console.error);
    }, [issueId]),
  );

  // --- WS event handlers ---

  useWSEvent(
    "comment:created",
    useCallback(
      (payload: unknown) => {
        const { comment } = payload as CommentCreatedPayload;
        if (comment.issue_id !== issueId) return;
        if (comment.author_type === "member" && comment.author_id === userId) return;
        setTimeline((prev) => {
          if (prev.some((e) => e.id === comment.id)) return prev;
          return [...prev, commentToTimelineEntry(comment)];
        });
      },
      [issueId, userId],
    ),
  );

  useWSEvent(
    "comment:updated",
    useCallback(
      (payload: unknown) => {
        const { comment } = payload as CommentUpdatedPayload;
        if (comment.issue_id === issueId) {
          setTimeline((prev) =>
            prev.map((e) => (e.id === comment.id ? commentToTimelineEntry(comment) : e)),
          );
        }
      },
      [issueId],
    ),
  );

  useWSEvent(
    "comment:deleted",
    useCallback(
      (payload: unknown) => {
        const { comment_id, issue_id } = payload as CommentDeletedPayload;
        if (issue_id === issueId) {
          setTimeline((prev) => {
            const idsToRemove = new Set<string>([comment_id]);
            let added = true;
            while (added) {
              added = false;
              for (const e of prev) {
                if (e.parent_id && idsToRemove.has(e.parent_id) && !idsToRemove.has(e.id)) {
                  idsToRemove.add(e.id);
                  added = true;
                }
              }
            }
            return prev.filter((e) => !idsToRemove.has(e.id));
          });
        }
      },
      [issueId],
    ),
  );

  useWSEvent(
    "activity:created",
    useCallback(
      (payload: unknown) => {
        const p = payload as ActivityCreatedPayload;
        if (p.issue_id !== issueId) return;
        const entry = p.entry;
        if (!entry || !entry.id) return;
        setTimeline((prev) => {
          if (prev.some((e) => e.id === entry.id)) return prev;
          return [...prev, entry];
        });
      },
      [issueId],
    ),
  );

  useWSEvent(
    "reaction:added",
    useCallback(
      (payload: unknown) => {
        const { reaction, issue_id } = payload as ReactionAddedPayload;
        if (issue_id !== issueId) return;
        if (reaction.actor_type === "member" && reaction.actor_id === userId) return;
        setTimeline((prev) =>
          prev.map((e) => {
            if (e.id !== reaction.comment_id) return e;
            const existing = e.reactions ?? [];
            if (existing.some((r) => r.id === reaction.id)) return e;
            return { ...e, reactions: [...existing, reaction] };
          }),
        );
      },
      [issueId, userId],
    ),
  );

  useWSEvent(
    "reaction:removed",
    useCallback(
      (payload: unknown) => {
        const p = payload as ReactionRemovedPayload;
        if (p.issue_id !== issueId) return;
        if (p.actor_type === "member" && p.actor_id === userId) return;
        setTimeline((prev) =>
          prev.map((e) => {
            if (e.id !== p.comment_id) return e;
            return {
              ...e,
              reactions: (e.reactions ?? []).filter(
                (r) => !(r.emoji === p.emoji && r.actor_type === p.actor_type && r.actor_id === p.actor_id),
              ),
            };
          }),
        );
      },
      [issueId, userId],
    ),
  );

  // --- Mutation functions ---

  const submitComment = useCallback(
    async (content: string, attachmentIds?: string[]) => {
      if (!content.trim() || submitting || !userId) return;
      setSubmitting(true);
      try {
        const comment = await api.createComment(issueId, content, undefined, undefined, attachmentIds);
        setTimeline((prev) => {
          if (prev.some((e) => e.id === comment.id)) return prev;
          return [...prev, commentToTimelineEntry(comment)];
        });
      } catch {
        toast.error("Failed to send comment");
      } finally {
        setSubmitting(false);
      }
    },
    [issueId, userId],
  );

  const submitReply = useCallback(
    async (parentId: string, content: string, attachmentIds?: string[]) => {
      if (!content.trim() || !userId) return;
      try {
        const comment = await api.createComment(issueId, content, "comment", parentId, attachmentIds);
        setTimeline((prev) => {
          if (prev.some((e) => e.id === comment.id)) return prev;
          return [...prev, commentToTimelineEntry(comment)];
        });
      } catch {
        toast.error("Failed to send reply");
      }
    },
    [issueId, userId],
  );

  const editComment = useCallback(
    async (commentId: string, content: string) => {
      // Optimistic: update content immediately
      let prevContent: string | undefined;
      setTimeline((prev) =>
        prev.map((e) => {
          if (e.id !== commentId) return e;
          prevContent = e.content;
          return { ...e, content, updated_at: new Date().toISOString() };
        }),
      );
      try {
        const updated = await api.updateComment(commentId, content);
        setTimeline((prev) =>
          prev.map((e) => (e.id === updated.id ? commentToTimelineEntry(updated) : e)),
        );
      } catch {
        // Rollback
        if (prevContent !== undefined) {
          setTimeline((prev) =>
            prev.map((e) => (e.id === commentId ? { ...e, content: prevContent! } : e)),
          );
        }
        toast.error("Failed to update comment");
      }
    },
    [],
  );

  const deleteComment = useCallback(
    async (commentId: string) => {
      // Capture entries for rollback
      let removedEntries: TimelineEntry[] = [];
      setTimeline((prev) => {
        const idsToRemove = new Set<string>([commentId]);
        let added = true;
        while (added) {
          added = false;
          for (const e of prev) {
            if (e.parent_id && idsToRemove.has(e.parent_id) && !idsToRemove.has(e.id)) {
              idsToRemove.add(e.id);
              added = true;
            }
          }
        }
        removedEntries = prev.filter((e) => idsToRemove.has(e.id));
        return prev.filter((e) => !idsToRemove.has(e.id));
      });
      try {
        await api.deleteComment(commentId);
      } catch {
        // Rollback: re-add removed entries
        setTimeline((prev) => [...prev, ...removedEntries]);
        toast.error("Failed to delete comment");
      }
    },
    [],
  );

  const toggleReaction = useCallback(
    async (commentId: string, emoji: string) => {
      if (!userId) return;
      const entry = timeline.find((e) => e.id === commentId);
      const existing = (entry?.reactions ?? []).find(
        (r) => r.emoji === emoji && r.actor_type === "member" && r.actor_id === userId,
      );
      if (existing) {
        setTimeline((prev) =>
          prev.map((e) => {
            if (e.id !== commentId) return e;
            return { ...e, reactions: (e.reactions ?? []).filter((r) => r.id !== existing.id) };
          }),
        );
        try {
          await api.removeReaction(commentId, emoji);
        } catch {
          setTimeline((prev) =>
            prev.map((e) => {
              if (e.id !== commentId) return e;
              return { ...e, reactions: [...(e.reactions ?? []), existing] };
            }),
          );
          toast.error("Failed to remove reaction");
        }
      } else {
        const tempReaction = {
          id: `temp-${Date.now()}`,
          comment_id: commentId,
          actor_type: "member",
          actor_id: userId,
          emoji,
          created_at: new Date().toISOString(),
        };
        setTimeline((prev) =>
          prev.map((e) => {
            if (e.id !== commentId) return e;
            return { ...e, reactions: [...(e.reactions ?? []), tempReaction] };
          }),
        );
        try {
          const reaction = await api.addReaction(commentId, emoji);
          setTimeline((prev) =>
            prev.map((e) => {
              if (e.id !== commentId) return e;
              return {
                ...e,
                reactions: (e.reactions ?? []).map((r) => (r.id === tempReaction.id ? reaction : r)),
              };
            }),
          );
        } catch {
          setTimeline((prev) =>
            prev.map((e) => {
              if (e.id !== commentId) return e;
              return { ...e, reactions: (e.reactions ?? []).filter((r) => r.id !== tempReaction.id) };
            }),
          );
          toast.error("Failed to add reaction");
        }
      }
    },
    [userId, timeline],
  );

  return {
    timeline,
    loading,
    submitting,
    submitComment,
    submitReply,
    editComment,
    deleteComment,
    toggleReaction,
  };
}
