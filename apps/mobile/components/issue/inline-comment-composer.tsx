/**
 * Inline issue-comment composer — thin wrapper around the shared
 * `<MessageComposer>` with comment-specific wiring:
 *
 *   - `onSubmit` → `useCreateComment(issueId).mutateAsync`
 *   - Reply target sourced from `useReplyTargetStore` (set by the
 *     comment long-press action sheet)
 *   - Mention picker path → `/[workspace]/mention-picker?mode=comment`
 *   - Upload context binds attachments to this issue
 *
 * All UI / state / chip plumbing lives in `MessageComposer`. The chat
 * composer (`components/chat/chat-composer.tsx`) uses the same component
 * with chat-mode props.
 */
import { useCallback } from "react";
import { useCreateComment } from "@/data/mutations/issues";
import { useReplyTargetStore } from "@/data/stores/reply-target-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { MessageComposer } from "@/components/composer/message-composer";
import { useT } from "@/lib/use-t";

export function InlineCommentComposer({
  issueId,
  onPublished,
}: {
  issueId: string;
  /** Called with the SERVER comment id after this user's own publish is
   *  accepted (RUYI-28 auto-expand). Not fired for optimistic inserts or
   *  other users' realtime arrivals — only the local success path. */
  onPublished?: (commentId: string) => void;
}) {
  const { t } = useT("issues");
  const createComment = useCreateComment(issueId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
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
      try {
        const created = await createComment.mutateAsync({
          content,
          parentId: replyTarget?.commentId,
          attachmentIds: attachmentIds.length > 0 ? attachmentIds : undefined,
        });
        if (created?.id) onPublished?.(created.id);
      } catch (err) {
        // Rethrow so MessageComposer's catch path restores text + chips.
        // The optimistic timeline row stays with its inline
        // Failed · Retry · Discard affordance.
        throw err;
      }
    },
    [createComment, replyTarget?.commentId, onPublished],
  );

  return (
    <MessageComposer
      onSubmit={onSubmit}
      mentionPickerPath={{
        pathname: "/[workspace]/mention-picker",
        params: { workspace: wsSlug ?? "", mode: "comment" },
      }}
      uploadContext={{ issueId }}
      placeholder={t("mobile.composer.placeholder", "Add a comment…")}
      pillLabel={t(
        "mobile.composer.pill_label",
        "Add a comment, @ to mention…",
      )}
      pillIcon="chatbubble-ellipses-outline"
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
