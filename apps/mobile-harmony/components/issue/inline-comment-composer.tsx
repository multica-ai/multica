/**
 * HarmonyOS port of apps/mobile/components/issue/inline-comment-composer.tsx
 * — inline issue-comment composer, thin wrapper around the shared
 * MessageComposer with comment-specific wiring:
 *
 *   - `onSubmit` → `useCreateComment(issueId).mutateAsync`
 *   - Reply target sourced from `useReplyTargetStore` (set by the comment
 *     long-press menu)
 *   - Mention picker sheet in "comment" mode (the composer owns the sheet —
 *     the iOS formSheet route became a BottomSheet)
 *   - Upload context binds attachments to this issue
 *
 * All UI / state / chip plumbing lives in MessageComposer.
 */
import React, { useCallback } from "react";
import { useCreateComment } from "@/data/mutations/issues";
import { useReplyTargetStore } from "@/data/stores/reply-target-store";
import { MessageComposer } from "@/components/composer/message-composer";

export function InlineCommentComposer({ issueId }: { issueId: string }) {
  const createComment = useCreateComment(issueId);
  const replyTarget = useReplyTargetStore((s) => s.target);
  const clearReplyTarget = useReplyTargetStore((s) => s.clear);

  const onSubmit = useCallback(
    async ({
      content,
      attachmentIds,
    }: {
      content: string;
      attachmentIds: string[];
    }) => {
      // Rethrow so MessageComposer's catch path restores text + chips.
      // The optimistic timeline row stays with its inline
      // Failed · Retry · Discard affordance.
      await createComment.mutateAsync({
        content,
        parentId: replyTarget?.commentId,
        attachmentIds: attachmentIds.length > 0 ? attachmentIds : undefined,
      });
    },
    [createComment, replyTarget?.commentId],
  );

  return (
    <MessageComposer
      onSubmit={onSubmit}
      mentionPickerMode="comment"
      uploadContext={{ issueId }}
      placeholder="Add a comment…"
      pillLabel="Add a comment, @ to mention…"
      pillIcon="chatbubble-outline"
      replyTarget={
        replyTarget
          ? {
              actorName: replyTarget.actorName,
              preview: replyTarget.preview,
            }
          : null
      }
      onClearReplyTarget={clearReplyTarget}
      expandTrigger={replyTarget?.commentId ?? null}
    />
  );
}
