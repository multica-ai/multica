"use client";

import type { ReactNode } from "react";
import { useRef, useState } from "react";
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
  /** Name of the currently selected agent, used in the placeholder. */
  agentName?: string;
  /** Rendered at the bottom-left of the input bar — typically the agent picker. */
  leftAdornment?: ReactNode;
}

export function ChatInput({
  onSend,
  onStop,
  isRunning,
  disabled,
  agentName,
  leftAdornment,
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
    if (!content || isRunning || disabled) {
      logger.debug("input.send skipped", {
        emptyContent: !content,
        isRunning,
        disabled,
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
    clearInputDraft(keyAtSend);
    setIsEmpty(true);
  };

  const placeholder = disabled
    ? "This session is archived"
    : agentName
      ? `Tell ${agentName} what to do…`
      : "Tell me what to do…";

  return (
    <div className="px-5 pb-3 pt-0">
      <div className="relative mx-auto flex min-h-16 max-h-40 w-full max-w-4xl flex-col rounded-lg bg-card pb-9 border-1 border-border transition-colors focus-within:border-brand">
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
        <div className="absolute bottom-1 right-1.5 flex items-center gap-1">
          <SubmitButton
            onClick={handleSend}
            disabled={isEmpty || !!disabled}
            running={isRunning}
            onStop={onStop}
          />
        </div>
      </div>
    </div>
  );
}
