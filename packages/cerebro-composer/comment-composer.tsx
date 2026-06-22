"use client";

// CommentComposer (FIR-1748) — issue comments, new + reply. The issue-specific
// chrome (pin, trigger-target bar, private-agent send confirm) lives here; the
// shared editor/upload/draft/expand machinery is in BaseComposer.

import { useCommentDraft } from "@multica/cerebro-comment-drafts";
import { usePrivateAgentSendConfirm } from "@multica/cerebro-access/views";
import {
  TriggerTargetBar,
  memberMentionMarkdown,
} from "@multica/views/issues/components";
import { BaseComposer } from "./base-composer";

export interface CommentComposerProps {
  issueId: string;
  onSubmit: (content: string, attachmentIds?: string[]) => Promise<void>;
  /** "new" = top-level comment composer; "reply" = threaded reply. */
  variant?: "new" | "reply";
  /** Enable the pin toggle (issue pages pass true on the new-comment field). */
  pinnable?: boolean;
  /** Agent the backend trigger wakes when the draft has no @agent mentions. */
  triggerAgentId?: string;
  autoFocus?: boolean;
  /** Reply only: scopes the draft to its thread. */
  rootCommentId?: string;
  /** Reply only: avatar of the replying actor. */
  avatar?: { type: string; id: string } | null;
  size?: "sm" | "default";
  placeholder?: string;
}

export function CommentComposer({
  issueId,
  onSubmit,
  variant = "new",
  pinnable = false,
  triggerAgentId,
  autoFocus = false,
  rootCommentId,
  avatar = null,
  size = "default",
  placeholder,
}: CommentComposerProps) {
  const isReply = variant === "reply";
  const draft = useCommentDraft(
    isReply ? `reply:${issueId}:${rootCommentId ?? ""}` : `new:${issueId}`,
  );
  const { confirmBeforeSend, confirmDialog } = usePrivateAgentSendConfirm({
    triggerAgentId,
  });

  return (
    <BaseComposer
      uploadIssueId={issueId}
      editorKey={isReply ? rootCommentId : issueId}
      draft={draft}
      onSubmit={onSubmit}
      trackAttachmentIds
      // FIR-1787 review: Jesper reverted FIR-1790 — the issue comment box keeps
      // its boxed card/border chrome (the bottom composer and replies are boxed,
      // matching the original thread design). Default frame=true on BaseComposer.
      placeholder={placeholder}
      autoFocus={autoFocus}
      size={size}
      avatar={isReply ? avatar : null}
      pin={isReply ? "sticky-bottom" : pinnable ? "fixed" : "none"}
      suppressPlaceholder={!!triggerAgentId}
      hasTopOverlay={!!triggerAgentId}
      confirmBeforeSend={confirmBeforeSend}
      confirmDialog={confirmDialog}
      renderTopOverlay={({ markdown, faded, insertText }) => (
        <TriggerTargetBar
          markdown={markdown}
          triggerAgentId={triggerAgentId}
          faded={faded}
          onTagOwner={(owner) => insertText(` ${memberMentionMarkdown(owner)} `)}
        />
      )}
    />
  );
}
