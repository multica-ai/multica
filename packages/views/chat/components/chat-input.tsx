"use client";

import type { ReactNode } from "react";
import { useRef, useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import { ContentEditor, type ContentEditorRef } from "../../editor";
import { SubmitButton } from "@multica/ui/components/common/submit-button";
import { useChatStore, DRAFT_NEW_SESSION } from "@multica/core/chat";
import { createLogger } from "@multica/core/logger";

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
}: ChatInputProps) {
  const editorRef = useRef<ContentEditorRef>(null);
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const selectedAgentId = useChatStore((s) => s.selectedAgentId);
  // Scope the new-chat draft by agent:
  //   1. Switching agents while composing a brand-new chat gives each
  //      agent its own draft (no cross-agent leakage).
  //   2. Tiptap's Placeholder extension is only applied at mount; this
  //      key changes on agent switch so the editor remounts and the
  //      `Tell {agent} what to do…` placeholder refreshes.
  const draftKey =
    activeSessionId ?? `${DRAFT_NEW_SESSION}:${selectedAgentId ?? ""}`;
  // Select a primitive — empty-string fallback keeps referential stability.
  const inputDraft = useChatStore((s) => s.inputDrafts[draftKey] ?? "");
  const setInputDraft = useChatStore((s) => s.setInputDraft);
  const clearInputDraft = useChatStore((s) => s.clearInputDraft);
  const [isEmpty, setIsEmpty] = useState(!inputDraft.trim());

  const handleSend = () => {
    const content = editorRef.current?.getMarkdown()?.replace(/(\n\s*)+$/, "").trim();
    if (!content || isRunning || disabled || noAgent) {
      logger.debug("input.send skipped", {
        emptyContent: !content,
        isRunning,
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
    ? "Create an agent to start chatting"
    : disabled
      ? "This session is archived"
      : agentName
        ? `Tell ${agentName} what to do…`
        : "Tell me what to do…";

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
            key={draftKey}
            ref={editorRef}
            defaultValue={inputDraft}
            placeholder={placeholder}
            onUpdate={(md) => {
              setIsEmpty(!md.trim());
              setInputDraft(draftKey, md);
            }}
            onSubmit={handleSend}
            debounceMs={100}
            // Chat is short-form — the floating formatting toolbar is
            // more distraction than feature here.
            showBubbleMenu={false}
            // Enter sends; Shift-Enter inserts a hard break.
            submitOnEnter
          />
        </div>
        {leftAdornment && (
          <div className="absolute bottom-1.5 left-2 flex items-center">
            {leftAdornment}
          </div>
        )}
        <div className="absolute bottom-1 right-1.5 flex items-center gap-2">
          {rightAdornment}
          <SubmitButton
            onClick={handleSend}
            disabled={isEmpty || !!disabled || !!noAgent}
            running={isRunning}
            onStop={onStop}
          />
        </div>
      </div>
    </div>
  );
}
