"use client";

import { useState, useCallback, useMemo } from "react";
import { useQuery, useQueryClient, useMutationState } from "@tanstack/react-query";
import type { Comment, TimelineEntry, Reaction } from "@multica/core/types";
import type {
  CommentCreatedPayload,
  CommentUpdatedPayload,
  CommentDeletedPayload,
  ActivityCreatedPayload,
  ReactionAddedPayload,
  ReactionRemovedPayload,
} from "@multica/core/types";
import { issueTimelineOptions, issueKeys } from "@multica/core/issues/queries";
import {
  useCreateComment,
  useUpdateComment,
  useDeleteComment,
  useToggleCommentReaction,
  type ToggleCommentReactionVars,
} from "@multica/core/issues/mutations";
import { useWSEvent, useWSReconnect } from "@multica/core/realtime";
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
    attachments: c.attachments ?? [],
  };
}

export function useIssueTimeline(issueId: string, userId?: string) {
  const qc = useQueryClient();
  const { data: timeline = [], isLoading: loading } = useQuery(
    issueTimelineOptions(issueId),
  );
  const [submitting, setSubmitting] = useState(false);

  const createCommentMutation = useCreateComment(issueId);
  const updateCommentMutation = useUpdateComment(issueId);
  const deleteCommentMutation = useDeleteComment(issueId);
  const toggleReactionMutation = useToggleCommentReaction(issueId);

  // Reconnect recovery
  useWSReconnect(
    useCallback(() => {
      qc.invalidateQueries({ queryKey: issueKeys.timeline(issueId) });
    }, [qc, issueId]),
  );

  // --- WS event handlers ---

  useWSEvent(
    "comment:created",
    useCallback(
      (payload: unknown) => {
        const { comment } = payload as CommentCreatedPayload;
        if (comment.issue_id !== issueId) return;
        qc.setQueryData<TimelineEntry[]>(
          issueKeys.timeline(issueId),
          (old) => {
            if (!old) return old;
            if (old.some((e) => e.id === comment.id)) return old;
            return [...old, commentToTimelineEntry(comment)];
          },
        );
      },
      [qc, issueId],
    ),
  );

  useWSEvent(
    "comment:updated",
    useCallback(
      (payload: unknown) => {
        const { comment } = payload as CommentUpdatedPayload;
        if (comment.issue_id === issueId) {
          qc.setQueryData<TimelineEntry[]>(
            issueKeys.timeline(issueId),
            (old) =>
              old?.map((e) =>
                e.id === comment.id ? commentToTimelineEntry(comment) : e,
              ),
          );
        }
      },
      [qc, issueId],
    ),
  );

  useWSEvent(
    "comment:deleted",
    useCallback(
      (payload: unknown) => {
        const { comment_id, issue_id } = payload as CommentDeletedPayload;
        if (issue_id === issueId) {
          qc.setQueryData<TimelineEntry[]>(
            issueKeys.timeline(issueId),
            (old) => {
              if (!old) return old;
              const idsToRemove = new Set<string>([comment_id]);
              let added = true;
              while (added) {
                added = false;
                for (const e of old) {
                  if (
                    e.parent_id &&
                    idsToRemove.has(e.parent_id) &&
                    !idsToRemove.has(e.id)
                  ) {
                    idsToRemove.add(e.id);
                    added = true;
                  }
                }
              }
              return old.filter((e) => !idsToRemove.has(e.id));
            },
          );
        }
      },
      [qc, issueId],
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
        qc.setQueryData<TimelineEntry[]>(
          issueKeys.timeline(issueId),
          (old) => {
            if (!old) return old;
            if (old.some((e) => e.id === entry.id)) return old;
            return [...old, entry];
          },
        );
      },
      [qc, issueId],
    ),
  );

  useWSEvent(
    "reaction:added",
    useCallback(
      (payload: unknown) => {
        const { reaction, issue_id } = payload as ReactionAddedPayload;
        if (issue_id !== issueId) return;
        qc.setQueryData<TimelineEntry[]>(
          issueKeys.timeline(issueId),
          (old) =>
            old?.map((e) => {
              if (e.id !== reaction.comment_id) return e;
              const existing = e.reactions ?? [];
              if (existing.some((r) => r.id === reaction.id)) return e;
              return { ...e, reactions: [...existing, reaction] };
            }),
        );
      },
      [qc, issueId],
    ),
  );

  useWSEvent(
    "reaction:removed",
    useCallback(
      (payload: unknown) => {
        const p = payload as ReactionRemovedPayload;
        if (p.issue_id !== issueId) return;
        qc.setQueryData<TimelineEntry[]>(
          issueKeys.timeline(issueId),
          (old) =>
            old?.map((e) => {
              if (e.id !== p.comment_id) return e;
              return {
                ...e,
                reactions: (e.reactions ?? []).filter(
                  (r) =>
                    !(
                      r.emoji === p.emoji &&
                      r.actor_type === p.actor_type &&
                      r.actor_id === p.actor_id
                    ),
                ),
              };
            }),
        );
      },
      [qc, issueId],
    ),
  );

  // --- Mutation functions ---

  const submitComment = useCallback(
    async (content: string, attachmentIds?: string[]) => {
      if (!content.trim() || submitting || !userId) return;
      setSubmitting(true);
      try {
        await createCommentMutation.mutateAsync({
          content,
          attachmentIds,
        });
      } catch {
        toast.error("Failed to send comment");
      } finally {
        setSubmitting(false);
      }
    },
    [userId, submitting, createCommentMutation],
  );

  const submitReply = useCallback(
    async (parentId: string, content: string, attachmentIds?: string[]) => {
      if (!content.trim() || !userId) return;
      try {
        await createCommentMutation.mutateAsync({
          content,
          type: "comment",
          parentId,
          attachmentIds,
        });
      } catch {
        toast.error("Failed to send reply");
      }
    },
    [userId, createCommentMutation],
  );

  const editComment = useCallback(
    async (commentId: string, content: string) => {
      try {
        await updateCommentMutation.mutateAsync({ commentId, content });
      } catch {
        toast.error("Failed to update comment");
      }
    },
    [updateCommentMutation],
  );

  const deleteComment = useCallback(
    async (commentId: string) => {
      try {
        await deleteCommentMutation.mutateAsync(commentId);
      } catch {
        toast.error("Failed to delete comment");
      }
    },
    [deleteCommentMutation],
  );

  // --- Optimistic UI derivation for comment reactions ---
  // Instead of writing temp data into the cache (which races with WS events),
  // derive optimistic state at render time from pending mutation variables.

  const pendingReactionVars = useMutationState({
    filters: {
      mutationKey: ["toggleCommentReaction", issueId],
      status: "pending",
    },
    select: (m) =>
      m.state.variables as ToggleCommentReactionVars | undefined,
  });

  const optimisticTimeline = useMemo(() => {
    if (pendingReactionVars.length === 0) return timeline;

    return timeline.map((entry) => {
      const pendingForEntry = pendingReactionVars.filter(
        (v) => v && v.commentId === entry.id,
      );
      if (pendingForEntry.length === 0) return entry;

      let reactions = entry.reactions ?? [];
      for (const vars of pendingForEntry) {
        if (!vars) continue;
        if (vars.existing) {
          // Pending removal
          reactions = reactions.filter((r) => r.id !== vars.existing!.id);
        } else {
          // Pending add — skip if server already has it (WS arrived first)
          const alreadyExists = reactions.some(
            (r) =>
              r.emoji === vars.emoji &&
              r.actor_type === "member" &&
              r.actor_id === userId,
          );
          if (!alreadyExists) {
            reactions = [
              ...reactions,
              {
                id: `optimistic-${vars.emoji}`,
                comment_id: vars.commentId,
                actor_type: "member",
                actor_id: userId ?? "",
                emoji: vars.emoji,
                created_at: "",
              },
            ];
          }
        }
      }
      return { ...entry, reactions };
    });
  }, [timeline, pendingReactionVars, userId]);

  const toggleReaction = useCallback(
    async (commentId: string, emoji: string) => {
      if (!userId) return;
      // Read from server timeline (not optimistic) to find the real reaction
      const entry = timeline.find((e) => e.id === commentId);
      const existing: Reaction | undefined = (entry?.reactions ?? []).find(
        (r) =>
          r.emoji === emoji &&
          r.actor_type === "member" &&
          r.actor_id === userId,
      );
      toggleReactionMutation.mutate({ commentId, emoji, existing });
    },
    [userId, timeline, toggleReactionMutation],
  );

  return {
    timeline: optimisticTimeline,
    loading,
    submitting,
    submitComment,
    submitReply,
    editComment,
    deleteComment,
    toggleReaction,
  };
}
