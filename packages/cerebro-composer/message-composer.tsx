"use client";

// MessageComposer (FIR-1748) — push/pull communication: Channels, DMs, agent
// Chat, and their threads. This is the single message field for everything that
// is a back-and-forth conversation (as opposed to issue comments, which use
// CommentComposer). Channels/DMs stop borrowing the issue comment composer.
//
// Draft store differs by surface, so the caller may inject a `draft` handle
// (chat builds one from useChatStore, per session + agent). When omitted, the
// composer persists drafts via useCommentDraft scoped by `draftKey`.

import { forwardRef, type ReactNode } from "react";
import { useCommentDraft } from "@multica/cerebro-comment-drafts";
import { type CommentDraftKey } from "@multica/core/issues/stores";
import {
  BaseComposer,
  type ComposerDraftHandle,
  type ComposerHandle,
  type ComposerMentionItem,
} from "./base-composer";

export interface MessageComposerProps {
  /** Conversation identity (draft fallback key + editor remount). For chat this
   *  is a synthetic session draft id; pass the matching `draft` handle. */
  conversationId: string;
  /** Issue/channel id uploads attach to. Channels/DMs pass the channel id;
   *  chat omits it (no backing issue). */
  uploadIssueId?: string;
  onSubmit: (content: string, attachmentIds?: string[]) => void | Promise<void>;
  /** Forward attachment ids on send (channels/DMs). Chat passes content only. */
  trackAttachmentIds?: boolean;

  /** Injected draft handle (chat). When absent a comment-draft is used. */
  draft?: ComposerDraftHandle;
  /** Draft scope key when no `draft` is injected, e.g. `new:${channelId}`. */
  draftKey?: CommentDraftKey;
  /** Remount key (session/channel/thread switch). */
  editorKey?: string;

  placeholder?: string;
  autoFocus?: boolean;
  disabled?: boolean;
  noAgent?: boolean;
  onTyping?: () => void;

  // chat affordances
  enableSlashCommands?: boolean;
  mentionContextItems?: ComposerMentionItem[];
  topSlot?: ReactNode;
  leftAdornment?: ReactNode;
  rightAdornment?: ReactNode;
  isRunning?: boolean;
  onStop?: () => void;
  dictationRow?: ReactNode;
  transcribingSlot?: ReactNode;

  /** Reply only: avatar of the replying actor. */
  avatar?: { type: string; id: string } | null;
  size?: "sm" | "default";
  /** Blur after send (chat). */
  blurOnSubmit?: boolean;
}

export const MessageComposer = forwardRef<ComposerHandle, MessageComposerProps>(function MessageComposer({
  conversationId,
  uploadIssueId,
  onSubmit,
  trackAttachmentIds = true,
  draft,
  draftKey,
  editorKey,
  placeholder,
  autoFocus = false,
  disabled = false,
  noAgent = false,
  onTyping,
  enableSlashCommands = false,
  mentionContextItems,
  topSlot,
  leftAdornment,
  rightAdornment,
  isRunning = false,
  onStop,
  dictationRow,
  transcribingSlot,
  avatar = null,
  size = "default",
  blurOnSubmit = false,
}: MessageComposerProps, ref) {
  // Always call the hook (rules of hooks); prefer the injected handle when the
  // caller owns the draft store (chat).
  const fallbackDraft = useCommentDraft(draftKey ?? `new:${conversationId}`);
  const effectiveDraft = draft ?? fallbackDraft;

  return (
    <BaseComposer
      ref={ref}
      uploadIssueId={uploadIssueId}
      editorKey={editorKey}
      draft={effectiveDraft}
      onSubmit={onSubmit}
      trackAttachmentIds={trackAttachmentIds}
      placeholder={placeholder}
      autoFocus={autoFocus}
      disabled={disabled}
      noAgent={noAgent}
      onTyping={onTyping}
      enableSlashCommands={enableSlashCommands}
      // Chat is short-form — the floating formatting toolbar is more
      // distraction than feature there; channels keep the default.
      showBubbleMenu={enableSlashCommands ? false : undefined}
      mentionMode={mentionContextItems ? "context" : "default"}
      mentionContextItems={mentionContextItems}
      dictation={dictationRow ? "action-row" : "corner"}
      pin="none"
      avatar={avatar}
      size={size}
      topSlot={topSlot}
      leftAdornment={leftAdornment}
      rightAdornment={rightAdornment}
      isRunning={isRunning}
      onStop={onStop}
      dictationRow={dictationRow}
      transcribingSlot={transcribingSlot}
      blurOnSubmit={blurOnSubmit}
    />
  );
});
