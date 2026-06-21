"use client";

// ChatComposer (FIR-1748) — agent Chat preset over the shared MessageComposer.
// Keeps ChatInput's exact prop API so chat-window / inbox-chat-panel swap 1:1.
// The chat-specific bits live here: per-session+agent draft (useChatStore),
// the one-shot dictation mic with its transcribing skeleton, the agent-aware
// placeholder, and blur-after-send. Everything else (editor, upload, height
// cap, slash commands, context mentions, stop button, adornments) is the
// shared composer.

import { type ReactNode, useCallback, useRef, useState } from "react";
import { useChatStore, DRAFT_NEW_SESSION } from "@multica/core/chat";
import {
  EditorDictationMic,
  TranscribingSkeleton,
  type DictationStatus,
} from "@multica/cerebro-dictation";
import { useT } from "@multica/views/i18n";
import { MessageComposer } from "./message-composer";
import {
  type ComposerDraftHandle,
  type ComposerHandle,
  type ComposerMentionItem,
} from "./base-composer";

export interface ChatComposerProps {
  onSend: (content: string) => void;
  onStop?: () => void;
  isRunning?: boolean;
  disabled?: boolean;
  noAgent?: boolean;
  agentName?: string;
  leftAdornment?: ReactNode;
  rightAdornment?: ReactNode;
  topSlot?: ReactNode;
  autoFocus?: boolean;
  /** Embedded panels pass the owning session id so drafts don't read the
   *  global active session. */
  draftSessionId?: string | null;
  /** Chat @ suggestions: current/recent issue/project entries. */
  contextItems?: ComposerMentionItem[];
}

export function ChatComposer({
  onSend,
  onStop,
  isRunning,
  disabled,
  noAgent,
  agentName,
  leftAdornment,
  rightAdornment,
  topSlot,
  autoFocus = false,
  draftSessionId,
  contextItems,
}: ChatComposerProps) {
  const { t } = useT("chat");
  const composerRef = useRef<ComposerHandle>(null);

  // Scope the new-chat draft by agent so switching agents while composing a
  // brand-new chat gives each agent its own draft (matches ChatInput).
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const selectedAgentId = useChatStore((s) => s.selectedAgentId);
  const scopedSessionId =
    draftSessionId === undefined ? activeSessionId : draftSessionId;
  const draftKey =
    scopedSessionId ?? `${DRAFT_NEW_SESSION}:${selectedAgentId ?? ""}`;
  const inputDraft = useChatStore((s) => s.inputDrafts[draftKey] ?? "");
  const setInputDraft = useChatStore((s) => s.setInputDraft);
  const clearInputDraft = useChatStore((s) => s.clearInputDraft);

  // The handle closes over this render's draftKey, so clear() after onSend
  // targets the key the message was composed under — even when onSend creates
  // a new session and mutates activeSessionId synchronously.
  const draft: ComposerDraftHandle = {
    defaultValue: inputDraft,
    save: (md) => setInputDraft(draftKey, md),
    clear: () => clearInputDraft(draftKey),
    saved: false,
  };

  const [transcribing, setTranscribing] = useState(false);
  const handleDictationStatus = useCallback((s: DictationStatus) => {
    setTranscribing(s === "transcribing");
  }, []);

  const placeholder = noAgent
    ? t(($) => $.input.placeholder_no_agent)
    : disabled
      ? t(($) => $.input.placeholder_archived)
      : agentName
        ? t(($) => $.input.placeholder_named, { name: agentName })
        : t(($) => $.input.placeholder_default);

  // FIR-1748 follow-up: the agent selector renders as a "Replying to"-style
  // target bar at the TOP of the field (mirrors the issue-comment trigger bar),
  // not as a bottom-left avatar. The passed `leftAdornment` is the interactive
  // agent picker; we wrap it in the labelled chip and keep it pointer-active
  // even while the (no-agent) field is dimmed.
  const agentBar = leftAdornment ? (
    <div className="flex items-center gap-1.5 px-3 pt-2 text-[11px] text-muted-foreground">
      <span className="shrink-0 font-medium">
        {noAgent ? "Pick an agent" : "Talking to"}
      </span>
      <span className="pointer-events-auto inline-flex min-w-0 max-w-full items-center gap-1 rounded-md border border-emerald-500/30 bg-emerald-500/10 px-1.5 py-0.5 font-medium text-emerald-700 dark:text-emerald-300">
        {leftAdornment}
      </span>
    </div>
  ) : null;

  const mergedTopSlot =
    agentBar || topSlot ? (
      <>
        {agentBar}
        {topSlot}
      </>
    ) : undefined;

  return (
    <MessageComposer
      ref={composerRef}
      // Chat has no backing issue: identity is the draft key, uploads carry no issueId.
      conversationId={draftKey}
      editorKey={draftKey}
      draft={draft}
      onSubmit={(content) => onSend(content)}
      trackAttachmentIds={false}
      placeholder={placeholder}
      autoFocus={autoFocus}
      disabled={disabled}
      noAgent={noAgent}
      enableSlashCommands
      mentionContextItems={contextItems}
      topSlot={mergedTopSlot}
      rightAdornment={rightAdornment}
      isRunning={isRunning}
      onStop={onStop}
      blurOnSubmit
      transcribingSlot={transcribing ? <TranscribingSkeleton /> : null}
      dictationRow={
        <EditorDictationMic
          disabled={!!disabled || !!noAgent}
          onTranscribed={(text) => composerRef.current?.insertText(text)}
          onStatusChange={handleDictationStatus}
        />
      }
    />
  );
}
