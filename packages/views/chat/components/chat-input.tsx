"use client";

// CEREBRO-PATCH(chat-input-mcp-onboarding): cerebro modification of upstream file

import type { ReactNode } from "react";
import { useCallback, useRef, useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import {
  ContentEditor,
  type ContentEditorRef,
  useFileDropZone,
  FileDropOverlay,
} from "../../editor";
import { SubmitButton } from "@multica/ui/components/common/submit-button";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { Button } from "@multica/ui/components/ui/button";
import { Square } from "lucide-react";
import { useChatStore, DRAFT_NEW_SESSION } from "@multica/core/chat";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { api } from "@multica/core/api";
import { createLogger } from "@multica/core/logger";
import { useSubmitOnEnter } from "@multica/cerebro-preferences/views";
import { useT } from "../../i18n";

const logger = createLogger("chat.ui");

interface ChatInputProps {
  onSend: (content: string) => void;
  onStop?: () => void;
  isRunning?: boolean;
  disabled?: boolean;
  /** True when the user has no agent available — disables the editor and
   *  surfaces a distinct placeholder. Kept separate from `disabled` so
   *  archived-session copy stays untouched. */
  noAgent?: boolean;
  /** Name of the currently selected agent, used in the placeholder. */
  agentName?: string;
  /** Rendered at the bottom-left of the input bar — typically the agent picker. */
  leftAdornment?: ReactNode;
  /** Rendered just before the submit button — used for context-anchor action. */
  rightAdornment?: ReactNode;
  /** Rendered inside the rounded container, above the editor — attached
   *  context cards, drafts, etc. */
  topSlot?: ReactNode;
  // CEREBRO-PATCH(input-autofocus): JEH-756 — when true, focus the editor on
  // mount and on session/agent switch so the user can start typing without
  // an extra click. Skipped if a dialog is in front.
  autoFocus?: boolean;
  // CEREBRO-PATCH(chat-session-scoped-draft): JEH-806 — embedded panels can pass
  // the owning session id so drafts do not read the global active session.
  draftSessionId?: string | null;
}

export function ChatInput({
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
}: ChatInputProps) {
  const { t } = useT("chat");
  const editorRef = useRef<ContentEditorRef>(null);
  const submitOnEnter = useSubmitOnEnter();
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const selectedAgentId = useChatStore((s) => s.selectedAgentId);
  // Scope the new-chat draft by agent:
  //   1. Switching agents while composing a brand-new chat gives each
  //      agent its own draft (no cross-agent leakage).
  //   2. Tiptap's Placeholder extension is only applied at mount; this
  //      key changes on agent switch so the editor remounts and the
  //      `Tell {agent} what to do…` placeholder refreshes.
  const scopedSessionId =
    draftSessionId === undefined ? activeSessionId : draftSessionId;
  const draftKey =
    scopedSessionId ?? `${DRAFT_NEW_SESSION}:${selectedAgentId ?? ""}`;
  // Select a primitive — empty-string fallback keeps referential stability.
  const inputDraft = useChatStore((s) => s.inputDrafts[draftKey] ?? "");
  const setInputDraft = useChatStore((s) => s.setInputDraft);
  const clearInputDraft = useChatStore((s) => s.clearInputDraft);
  const [isEmpty, setIsEmpty] = useState(!inputDraft.trim());

  const { uploadWithToast } = useFileUpload(api);
  const handleUpload = useCallback(
    (file: File) => uploadWithToast(file),
    [uploadWithToast],
  );

  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: (files) => files.forEach((f) => editorRef.current?.uploadFile(f)),
    enabled: !disabled,
  });

  const handleSend = () => {
    const content = editorRef.current?.getMarkdown()?.replace(/(\n\s*)+$/, "").trim();
    // CEREBRO-PATCH(chat-input-mcp-onboarding): Sending while the agent is mid-stream is
    // allowed: the backend coalesces the new message into the next turn
    // (see EnqueueChatTask). Block empty input, archived sessions, and no-agent state.
    if (!content || disabled || noAgent) {
      logger.debug("input.send skipped", {
        emptyContent: !content,
        disabled,
        noAgent,
      });
      return;
    }
    // Capture draft key BEFORE onSend — creating a new session mutates
    // activeSessionId synchronously, so reading it after onSend would point
    // at the new session and leave the old draft orphaned.
    const keyAtSend = draftKey;
    logger.info("input.send", { contentLength: content.length, draftKey: keyAtSend });
    onSend(content);
    editorRef.current?.clearContent();
    // Drop focus so the caret doesn't keep blinking under the StatusPill /
    // streaming reply that's about to take over the user's attention. The
    // input is also `disabled` once isRunning flips, and a focused-but-
    // disabled editor reads as a stale cursor. We deliberately don't auto-
    // refocus on completion — that would interrupt the user if they're
    // selecting text from the assistant reply; one click to refocus is
    // a fair price for not stealing focus mid-action.
    editorRef.current?.blur();
    clearInputDraft(keyAtSend);
    setIsEmpty(true);
  };

  const placeholder = noAgent
    ? t(($) => $.input.placeholder_no_agent)
    : disabled
      ? t(($) => $.input.placeholder_archived)
      : agentName
        ? t(($) => $.input.placeholder_named, { name: agentName })
        : t(($) => $.input.placeholder_default);

  return (
    <div
      className={cn(
        "px-5 pb-3 pt-0",
        // Outer wrapper carries the disabled cursor. Inner card sets
        // pointer-events-none, which suppresses hover (and therefore
        // any cursor of its own) — splitting the two layers lets hover
        // bubble back here so the browser actually reads cursor.
        noAgent && "cursor-not-allowed",
      )}
    >
      <div
        {...dropZoneProps}
        className={cn(
          "relative mx-auto flex min-h-16 max-h-40 w-full max-w-4xl flex-col rounded-lg bg-card pb-9 border-1 border-border transition-colors focus-within:border-brand",
          // Visual + interaction lock when there's no agent. We don't
          // toggle ContentEditor's editable mode (Tiptap can't switch
          // cleanly post-mount, and the prop has been removed); instead
          // we drop pointer events at the wrapper level so clicks miss
          // the editor entirely, and dim the surface so it reads as
          // "disabled" rather than "broken".
          noAgent && "pointer-events-none opacity-60",
        )}
        aria-disabled={noAgent || undefined}
      >
        {topSlot}
        <div className="flex-1 min-h-0 overflow-y-auto px-3 py-2">
          <ContentEditor
            // Remount the editor when the active session changes so its
            // uncontrolled defaultValue picks up the new session's draft.
            // Also re-evaluates `autoFocus` for the new session/agent —
            // Tiptap reads autofocus once at create time.
            key={draftKey}
            ref={editorRef}
            defaultValue={inputDraft}
            placeholder={placeholder}
            onUpdate={(md) => {
              setIsEmpty(!md.trim());
              setInputDraft(draftKey, md);
            }}
            onSubmit={handleSend}
            onUploadFile={handleUpload}
            debounceMs={100}
            // Chat is short-form — the floating formatting toolbar is
            // more distraction than feature here.
            showBubbleMenu={false}
            // Driven by user preference (Settings → Account):
            //   true  → Enter sends, Shift+Enter inserts newline
            //   false → Cmd/Ctrl+Enter sends, Enter inserts newline
            submitOnEnter={submitOnEnter}
            // CEREBRO-PATCH(input-autofocus): JEH-756 — Tiptap-native
            // autofocus. Gated here so archived sessions and no-agent
            // surfaces don't pull focus into a non-functional editor.
            autoFocus={autoFocus && !disabled && !noAgent}
          />
        </div>
        {leftAdornment && (
          <div className="absolute bottom-1.5 left-2 flex items-center">
            {leftAdornment}
          </div>
        )}
        {/* CEREBRO-PATCH(file-upload-button-api): wide mobile gap separates
            paperclip from the send button on touch screens, with the new
            onAttach/onEmbed FileUploadButton API. */}
        <div className="absolute bottom-1 right-1.5 flex items-center gap-4 sm:gap-1">
          <FileUploadButton
            size="sm"
            disabled={!!disabled}
            onAttach={(files) => files.forEach((f) => editorRef.current?.uploadFile(f))}
            onEmbed={(files) => files.forEach((f) => editorRef.current?.uploadFile(f, { embedImage: true }))}
          />
          {rightAdornment}
          {isRunning && onStop && (
            <Button
              size="icon-sm"
              variant="secondary"
              onClick={onStop}
              aria-label="Stop"
            >
              <Square className="fill-current" />
            </Button>
          )}
          <SubmitButton
            onClick={handleSend}
            disabled={isEmpty || !!disabled || !!noAgent}
          />
        </div>
        {isDragOver && <FileDropOverlay />}
      </div>
    </div>
  );
}
