"use client";

import { useEffect, useRef, useState, useCallback, useMemo } from "react";
import {
  useQuery,
  useQueryClient,
  useMutationState,
} from "@tanstack/react-query";
import type {
  Comment,
  TimelineEntry,
  Reaction,
} from "@multica/core/types";
import type {
  CommentCreatedPayload,
  CommentUpdatedPayload,
  CommentDeletedPayload,
  CommentResolvedPayload,
  CommentUnresolvedPayload,
  ActivityCreatedPayload,
  ReactionAddedPayload,
  ReactionRemovedPayload,
} from "@multica/core/types";
import {
  issueTimelineOptions,
  issueKeys,
} from "@multica/core/issues/queries";
import {
  useCreateComment,
  useUpdateComment,
  useDeleteComment,
  useResolveComment,
  useToggleCommentReaction,
  type ToggleCommentReactionVars,
} from "@multica/core/issues/mutations";
import { useWSEvent, useWSReconnect } from "@multica/core/realtime";
import { toast } from "sonner";
import { useT } from "../../i18n";

type TLCache = TimelineEntry[];

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
    resolved_at: c.resolved_at,
    resolved_by_type: c.resolved_by_type,
    resolved_by_id: c.resolved_by_id,
  };
}

export function useIssueTimeline(issueId: string, userId?: string) {
  const { t } = useT("issues");
  const qc = useQueryClient();

  const query = useQuery(issueTimelineOptions(issueId));
  const { data, isLoading: loading } = query;

  const timeline = useMemo<TimelineEntry[]>(() => data ?? [], [data]);

  const [submitting, setSubmitting] = useState(false);

  // Stable mutation handles. TanStack v5 returns a fresh result wrapper from
  // useMutation per render, but the inner mutateAsync / mutate functions are
  // stable. Pull just those so the useCallback identities downstream don't
  // flip on every parent re-render — listing the whole mutation object would
  // defeat React.memo on CommentCard.
  const { mutateAsync: createComment } = useCreateComment(issueId);
  const { mutateAsync: updateComment } = useUpdateComment(issueId);
  const { mutateAsync: deleteCommentAsync } = useDeleteComment(issueId);
  const { mutateAsync: resolveCommentAsync } = useResolveComment(issueId);
  const { mutate: toggleCommentReaction } = useToggleCommentReaction(issueId);

  // Reconnect recovery: invalidate so the next render refetches the full
  // timeline. Cheaper than diffing across a possibly-long disconnect.
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
        qc.setQueryData<TLCache>(issueKeys.timeline(issueId), (old) => {
          const entry = commentToTimelineEntry(comment);
          if (!old) return [entry];
          if (old.some((e) => e.id === comment.id)) return old;
          return [...old, entry];
        });
      },
      [qc, issueId],
    ),
  );

  useWSEvent(
    "comment:updated",
    useCallback(
      (payload: unknown) => {
        const { comment } = payload as CommentUpdatedPayload;
        if (comment.issue_id !== issueId) return;
        qc.setQueryData<TLCache>(issueKeys.timeline(issueId), (old) =>
          old?.map((e) =>
            e.id === comment.id ? commentToTimelineEntry(comment) : e,
          ),
        );
      },
      [qc, issueId],
    ),
  );

  // Granular handlers for comment:resolved / comment:unresolved. The payload
  // carries the full Comment with the new resolved_at/resolved_by_* fields,
  // which `commentToTimelineEntry` already preserves, so the existing
  // entry can simply be replaced in place. Without these handlers the only
  // path that updated the cache was `useRealtimeSync`'s global invalidate,
  // which forces a full timeline refetch and busts every CommentCard memo.
  useWSEvent(
    "comment:resolved",
    useCallback(
      (payload: unknown) => {
        const { comment } = payload as CommentResolvedPayload;
        if (comment.issue_id !== issueId) return;
        qc.setQueryData<TLCache>(issueKeys.timeline(issueId), (old) =>
          old?.map((e) =>
            e.id === comment.id ? commentToTimelineEntry(comment) : e,
          ),
        );
      },
      [qc, issueId],
    ),
  );

  useWSEvent(
    "comment:unresolved",
    useCallback(
      (payload: unknown) => {
        const { comment } = payload as CommentUnresolvedPayload;
        if (comment.issue_id !== issueId) return;
        qc.setQueryData<TLCache>(issueKeys.timeline(issueId), (old) =>
          old?.map((e) =>
            e.id === comment.id ? commentToTimelineEntry(comment) : e,
          ),
        );
      },
      [qc, issueId],
    ),
  );

  useWSEvent(
    "comment:deleted",
    useCallback(
      (payload: unknown) => {
        const { comment_id, issue_id } = payload as CommentDeletedPayload;
        if (issue_id !== issueId) return;
        qc.setQueryData<TLCache>(issueKeys.timeline(issueId), (old) => {
          if (!old) return old;
          // Cascade through replies (full timeline now lives in this single
          // cache, so a flat sweep is sufficient).
          const idsToRemove = new Set<string>([comment_id]);
          let changed = true;
          while (changed) {
            changed = false;
            for (const e of old) {
              if (
                e.parent_id &&
                idsToRemove.has(e.parent_id) &&
                !idsToRemove.has(e.id)
              ) {
                idsToRemove.add(e.id);
                changed = true;
              }
            }
          }
          return old.filter((e) => !idsToRemove.has(e.id));
        });
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
        qc.setQueryData<TLCache>(issueKeys.timeline(issueId), (old) => {
          if (!old) return [entry];
          if (old.some((e) => e.id === entry.id)) return old;
          return [...old, entry];
        });
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
        qc.setQueryData<TLCache>(issueKeys.timeline(issueId), (old) =>
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
        qc.setQueryData<TLCache>(issueKeys.timeline(issueId), (old) =>
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
        await createComment({ content, attachmentIds });
      } catch {
        toast.error(t(($) => $.comment.send_failed));
      } finally {
        setSubmitting(false);
      }
    },
    [userId, submitting, createComment, t],
  );

  const submitReply = useCallback(
    async (parentId: string, content: string, attachmentIds?: string[]) => {
      if (!content.trim() || !userId) return;
      try {
        await createComment({
          content,
          type: "comment",
          parentId,
          attachmentIds,
        });
      } catch {
        toast.error(t(($) => $.comment.send_reply_failed));
      }
    },
    [userId, createComment, t],
  );

  const editComment = useCallback(
    async (commentId: string, content: string) => {
      try {
        await updateComment({ commentId, content });
      } catch {
        toast.error(t(($) => $.comment.update_failed));
      }
    },
    [updateComment, t],
  );

  const deleteComment = useCallback(
    async (commentId: string) => {
      try {
        await deleteCommentAsync(commentId);
      } catch {
        toast.error(t(($) => $.comment.delete_failed));
      }
    },
    [deleteCommentAsync, t],
  );

  const toggleResolveComment = useCallback(
    async (commentId: string, resolved: boolean) => {
      try {
        await resolveCommentAsync({ commentId, resolved });
      } catch {
        toast.error(
          resolved
            ? t(($) => $.comment.resolve.resolve_failed)
            : t(($) => $.comment.resolve.unresolve_failed),
        );
      }
    },
    [resolveCommentAsync, t],
  );

  // --- Optimistic UI for comment reactions ---
  // Derive at render time from pending mutation variables instead of writing
  // temp data into the cache (which would race with WS events).

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
          reactions = reactions.filter((r) => r.id !== vars.existing!.id);
        } else {
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

  // toggleReaction reads from a ref so its identity does not change with
  // every WS event. Without this every memoized CommentCard down-tree would
  // re-render on each timeline mutation, defeating the React.memo cost
  // savings on long timelines (#1968).
  const timelineRef = useRef(timeline);
  useEffect(() => {
    timelineRef.current = timeline;
  }, [timeline]);

  const toggleReaction = useCallback(
    async (commentId: string, emoji: string) => {
      if (!userId) return;
      const entry = timelineRef.current.find((e) => e.id === commentId);
      const existing: Reaction | undefined = (entry?.reactions ?? []).find(
        (r) =>
          r.emoji === emoji &&
          r.actor_type === "member" &&
          r.actor_id === userId,
      );
      toggleCommentReaction({ commentId, emoji, existing });
    },
    [userId, toggleCommentReaction],
  );

  return {
    timeline: optimisticTimeline,
    loading,
    submitting,
    submitComment,
    submitReply,
    editComment,
    deleteComment,
    toggleResolveComment,
    toggleReaction,
  };
}
