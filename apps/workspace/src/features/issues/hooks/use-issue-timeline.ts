"use client";

import { useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { Comment, TimelineEntry } from "@/shared/types";
import type {
  CommentCreatedPayload,
  CommentUpdatedPayload,
  CommentDeletedPayload,
  ActivityCreatedPayload,
  ReactionAddedPayload,
  ReactionRemovedPayload,
} from "@/shared/types";
import { useWSEvent, useWSReconnect } from "@/features/realtime";
import { toast } from "sonner";
import { queryKeys } from "@/shared/query";
import { useIssueTimelineMutations } from "../mutations";
import { useIssueTimelineQuery } from "../queries";

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
  const queryClient = useQueryClient();
  const timelineQuery = useIssueTimelineQuery(issueId);
  const {
    submitComment,
    submitReply,
    editComment,
    deleteComment,
    toggleCommentReaction,
    submitting,
  } = useIssueTimelineMutations(issueId);
  const timeline = timelineQuery.data ?? [];
  const loading = timelineQuery.isPending;

  // Reconnect recovery
  useWSReconnect(
    useCallback(() => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.issues.timeline(issueId) });
    }, [issueId, queryClient]),
  );

  // --- WS event handlers ---

  useWSEvent(
    "comment:created",
    useCallback(
      (payload: unknown) => {
        const { comment } = payload as CommentCreatedPayload;
        if (comment.issue_id !== issueId) return;
        if (comment.author_type === "member" && comment.author_id === userId) return;
        queryClient.setQueryData<TimelineEntry[]>(queryKeys.issues.timeline(issueId), (existing = []) => {
          if (existing.some((entry) => entry.id === comment.id)) return existing;
          return [...existing, commentToTimelineEntry(comment)];
        });
      },
      [issueId, queryClient, userId],
    ),
  );

  useWSEvent(
    "comment:updated",
    useCallback(
      (payload: unknown) => {
        const { comment } = payload as CommentUpdatedPayload;
        if (comment.issue_id === issueId) {
          queryClient.setQueryData<TimelineEntry[]>(queryKeys.issues.timeline(issueId), (existing = []) =>
            existing.map((entry) =>
              entry.id === comment.id ? commentToTimelineEntry(comment) : entry,
            ),
          );
        }
      },
      [issueId, queryClient],
    ),
  );

  useWSEvent(
    "comment:deleted",
    useCallback(
      (payload: unknown) => {
        const { comment_id, issue_id } = payload as CommentDeletedPayload;
        if (issue_id === issueId) {
          queryClient.setQueryData<TimelineEntry[]>(queryKeys.issues.timeline(issueId), (existing = []) => {
            const idsToRemove = new Set<string>([comment_id]);
            let added = true;
            while (added) {
              added = false;
              for (const entry of existing) {
                if (entry.parent_id && idsToRemove.has(entry.parent_id) && !idsToRemove.has(entry.id)) {
                  idsToRemove.add(entry.id);
                  added = true;
                }
              }
            }
            return existing.filter((entry) => !idsToRemove.has(entry.id));
          });
        }
      },
      [issueId, queryClient],
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
        queryClient.setQueryData<TimelineEntry[]>(queryKeys.issues.timeline(issueId), (existing = []) => {
          if (existing.some((item) => item.id === entry.id)) return existing;
          return [...existing, entry];
        });
      },
      [issueId, queryClient],
    ),
  );

  useWSEvent(
    "reaction:added",
    useCallback(
      (payload: unknown) => {
        const { reaction, issue_id } = payload as ReactionAddedPayload;
        if (issue_id !== issueId) return;
        if (reaction.actor_type === "member" && reaction.actor_id === userId) return;
        queryClient.setQueryData<TimelineEntry[]>(queryKeys.issues.timeline(issueId), (existing = []) =>
          existing.map((entry) => {
            if (entry.id !== reaction.comment_id) return entry;
            const reactions = entry.reactions ?? [];
            if (reactions.some((item) => item.id === reaction.id)) return entry;
            return { ...entry, reactions: [...reactions, reaction] };
          }),
        );
      },
      [issueId, queryClient, userId],
    ),
  );

  useWSEvent(
    "reaction:removed",
    useCallback(
      (payload: unknown) => {
        const p = payload as ReactionRemovedPayload;
        if (p.issue_id !== issueId) return;
        if (p.actor_type === "member" && p.actor_id === userId) return;
        queryClient.setQueryData<TimelineEntry[]>(queryKeys.issues.timeline(issueId), (existing = []) =>
          existing.map((entry) => {
            if (entry.id !== p.comment_id) return entry;
            return {
              ...entry,
              reactions: (entry.reactions ?? []).filter(
                (reaction) =>
                  !(reaction.emoji === p.emoji && reaction.actor_type === p.actor_type && reaction.actor_id === p.actor_id),
              ),
            };
          }),
        );
      },
      [issueId, queryClient, userId],
    ),
  );

  const submitCommentSafe = useCallback(
    async (content: string, attachmentIds?: string[]) => {
      if (!content.trim() || !userId) return;
      try {
        await submitComment(content, attachmentIds);
      } catch {
        toast.error("Failed to send comment");
      }
    },
    [submitComment, userId],
  );

  const submitReplySafe = useCallback(
    async (parentId: string, content: string, attachmentIds?: string[]) => {
      if (!content.trim() || !userId) return;
      try {
        await submitReply(parentId, content, attachmentIds);
      } catch {
        toast.error("Failed to send reply");
      }
    },
    [submitReply, userId],
  );

  const editCommentSafe = useCallback(
    async (commentId: string, content: string) => {
      try {
        await editComment(commentId, content);
      } catch {
        toast.error("Failed to update comment");
      }
    },
    [editComment],
  );

  const deleteCommentSafe = useCallback(
    async (commentId: string) => {
      try {
        await deleteComment(commentId);
      } catch {
        toast.error("Failed to delete comment");
      }
    },
    [deleteComment],
  );

  const toggleReaction = useCallback(
    async (commentId: string, emoji: string) => {
      if (!userId) return;
      const entry = timeline.find((item) => item.id === commentId);
      const existingReactionId = (entry?.reactions ?? []).find(
        (reaction) => reaction.emoji === emoji && reaction.actor_type === "member" && reaction.actor_id === userId,
      )?.id;

      try {
        await toggleCommentReaction(commentId, emoji, existingReactionId);
      } catch {
        toast.error(existingReactionId ? "Failed to remove reaction" : "Failed to add reaction");
      }
    },
    [timeline, toggleCommentReaction, userId],
  );

  return {
    timeline,
    loading,
    submitting,
    submitComment: submitCommentSafe,
    submitReply: submitReplySafe,
    editComment: editCommentSafe,
    deleteComment: deleteCommentSafe,
    toggleReaction,
  };
}
