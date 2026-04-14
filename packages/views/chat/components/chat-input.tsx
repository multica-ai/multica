"use client";

import { useRef, useState } from "react";
import { ContentEditor, type ContentEditorRef } from "../../editor";
import { SubmitButton } from "@multica/ui/components/common/submit-button";
import { useChatStore } from "@multica/core/chat";

interface ChatInputProps {
  onSend: (content: string) => void;
  onStop?: () => void;
  isRunning?: boolean;
  disabled?: boolean;
}

export function ChatInput({ onSend, onStop, isRunning, disabled }: ChatInputProps) {
  const editorRef = useRef<ContentEditorRef>(null);
  const inputDraft = useChatStore((s) => s.inputDraft);
  const setInputDraft = useChatStore((s) => s.setInputDraft);
  const clearInputDraft = useChatStore((s) => s.clearInputDraft);
  const [isEmpty, setIsEmpty] = useState(!inputDraft.trim());

  const handleSend = () => {
    const content = editorRef.current?.getMarkdown()?.replace(/(\n\s*)+$/, "").trim();
    if (!content || isRunning || disabled) return;
    onSend(content);
    editorRef.current?.clearContent();
    clearInputDraft();
    setIsEmpty(true);
  };

  return (
    <div className="p-2 pt-0">
      <div className="relative flex min-h-16 max-h-40 flex-col rounded-lg bg-card pb-8 border-1 border-border transition-colors focus-within:border-brand">
        <div className="flex-1 min-h-0 overflow-y-auto px-3 py-2">
          <ContentEditor
            ref={editorRef}
            defaultValue={inputDraft}
            placeholder={disabled ? "This session is archived" : "Ask Multica..."}
            onUpdate={(md) => {
              setIsEmpty(!md.trim());
              setInputDraft(md);
            }}
            onSubmit={handleSend}
            debounceMs={100}
          />
        </div>
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
