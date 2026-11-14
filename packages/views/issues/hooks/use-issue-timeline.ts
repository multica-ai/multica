"use client";

import { useEffect, useRef, useCallback, useMemo } from "react";
import { useQuery, useMutationState } from "@tanstack/react-query";
import type { TimelineEntry, Reaction } from "@multica/core/types";
import {
  issueTimelineOptions,
  issueCommentThreadOptions,
} from "@multica/core/issues/queries";
import {
  useCreateComment,
  useUpdateComment,
  useDeleteComment,
  useResolveComment,
  useToggleCommentReaction,
  type ToggleCommentReactionVars,
} from "@multica/core/issues/mutations";
import { mergeCommentThread } from "@multica/core/issues/comment-thread";
import { useIssueTimelineSync } from "@multica/core/issues/use-issue-timeline-sync";
import {
  unhandledCommentTriggerOutcomes,
  mentionLabelsByTarget,
} from "@multica/core/issues/comment-trigger-outcomes";
import { toast } from "sonner";
import { useT } from "../../i18n";
import { blockedShortReasonLabel } from "../blocked-trigger-copy";

export function useIssueTimeline(issueId: string, userId?: string, focusCommentId?: string) {
  const { t } = useT("issues");

  const query = useQuery(issueTimelineOptions(issueId));
  const { data, isLoading: loading, dataUpdatedAt } = query;

  const needsThread = !!focusCommentId && !loading && !data?.some((entry) => entry.id === focusCommentId);
  const threadQuery = useQuery({
    ...issueCommentThreadOptions(issueId, focusCommentId ?? ""),
    enabled: needsThread,
  });
  useEffect(() => {
    if (threadQuery.error && needsThread) toast.error(t(($) => $.detail.load_failed));
  }, [threadQuery.error, needsThread, t]);
  const timeline = useMemo<TimelineEntry[]>(
    () => mergeCommentThread(data ?? [], threadQuery.data ?? []),
    [data, threadQuery.data],
  );

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

  useIssueTimelineSync(issueId);

  // --- Mutation functions ---

  // The comment saved, but a mention did not clearly trigger (blocked, or an
  // unknown/future status we must not assume succeeded). Warn instead of a
  // silent no-op (MUL-4525 §2): the comment IS posted, but N explicitly-named
  // targets were not triggered.
  const warnUnhandledTriggers = useCallback(
    (triggerOutcomes: unknown, content?: string) => {
      const unhandled = unhandledCommentTriggerOutcomes(triggerOutcomes);
      if (unhandled.length === 0) return;
      // Name the target when a single mention was refused — the posted comment's
      // markup carries the label the user typed (the wire outcome omits it for
      // enumeration-safety). Several refusals fall back to a count.
      if (unhandled.length === 1) {
        const outcome = unhandled[0]!;
        const name = mentionLabelsByTarget(content ?? "").get(
          `${outcome.target_type}:${outcome.target_id}`,
        );
        if (name) {
          toast.warning(
            t(($) => $.comment.posted_partial_trigger_named, {
              name,
              reason: blockedShortReasonLabel(outcome.reason_code, t),
            }),
          );
          return;
        }
      }
      toast.warning(
        t(($) => $.comment.posted_partial_trigger, { count: unhandled.length }),
      );
    },
    [t],
  );

  // Returns true on success, false on failure. The composer keeps the user's
  // text (editor locked + button spinning) until this settles and clears only
  // on success — so a slow send no longer leaves the box full next to an
  // already-posted comment, and a failed send keeps the draft.
  const submitComment = useCallback(
    async (content: string, attachmentIds?: string[], suppressAgentIds?: string[], steerTaskIds?: string[]): Promise<string | false> => {
      if (!content.trim() || !userId) return false;
      try {
        const comment = await createComment({ content, attachmentIds, suppressAgentIds, steerTaskIds });
        warnUnhandledTriggers(comment?.trigger_outcomes, comment?.content);
        return comment.id;
      } catch (err) {
        toast.error(
          err instanceof Error && err.message
            ? err.message
            : t(($) => $.comment.send_failed),
        );
        return false;
      }
    },
    [userId, createComment, warnUnhandledTriggers, t],
  );

  const submitReply = useCallback(
    async (parentId: string, content: string, attachmentIds?: string[], suppressAgentIds?: string[], steerTaskIds?: string[]): Promise<string | false> => {
      if (!content.trim() || !userId) return false;
      try {
        const comment = await createComment({
          content,
          type: "comment",
          parentId,
          attachmentIds,
          suppressAgentIds,
          steerTaskIds,
        });
        warnUnhandledTriggers(comment?.trigger_outcomes, comment?.content);
        return comment.id;
      } catch (err) {
        toast.error(
          err instanceof Error && err.message
            ? err.message
            : t(($) => $.comment.send_reply_failed),
        );
        return false;
      }
    },
    [userId, createComment, warnUnhandledTriggers, t],
  );

  const editComment = useCallback(
    async (commentId: string, content: string, attachmentIds: string[], suppressAgentIds?: string[], contentBase?: string) => {
      const comment = await updateComment({
        commentId,
        content,
        attachmentIds,
        suppressAgentIds,
        contentBase,
      });
      warnUnhandledTriggers(comment?.trigger_outcomes, comment?.content);
    },
    [updateComment, warnUnhandledTriggers],
  );

  const deleteComment = useCallback(
    async (commentId: string) => {
      try {
        await deleteCommentAsync(commentId);
      } catch (err) {
        toast.error(
          err instanceof Error && err.message
            ? err.message
            : t(($) => $.comment.delete_failed),
        );
      }
    },
    [deleteCommentAsync, t],
  );

  const toggleResolveComment = useCallback(
    async (commentId: string, resolved: boolean) => {
      try {
        await resolveCommentAsync({ commentId, resolved });
      } catch (err) {
        toast.error(
          err instanceof Error && err.message
            ? err.message
            : resolved
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
    loading: loading || (needsThread && threadQuery.isPending),
    /**
     * When the cached timeline was last written (Query's `dataUpdatedAt`).
     * Lets a caller tell whether `timeline` already reflects a read it
     * started; `loading` cannot, as it is false while cached data refreshes.
     */
    dataUpdatedAt,
    submitComment,
    submitReply,
    editComment,
    deleteComment,
    toggleResolveComment,
    toggleReaction,
  };
}
