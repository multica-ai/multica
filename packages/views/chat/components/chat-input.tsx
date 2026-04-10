"use client";

import { useState, useRef, useCallback } from "react";
import { ArrowUp, Square } from "lucide-react";

interface ChatInputProps {
  onSend: (content: string) => void;
  onStop?: () => void;
  isRunning?: boolean;
  disabled?: boolean;
}

export function ChatInput({ onSend, onStop, isRunning, disabled }: ChatInputProps) {
  const [value, setValue] = useState("");
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const handleSend = useCallback(() => {
    const trimmed = value.trim();
    if (!trimmed || isRunning || disabled) return;
    onSend(trimmed);
    setValue("");
    if (textareaRef.current) {
      textareaRef.current.style.height = "auto";
    }
    textareaRef.current?.focus();
  }, [value, isRunning, disabled, onSend]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key === "Enter" && !e.shiftKey) {
        e.preventDefault();
        handleSend();
      }
    },
    [handleSend],
  );

  const handleInput = useCallback(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = Math.min(el.scrollHeight, 120) + "px";
  }, []);

  return (
    <div className="border-t bg-muted/30 p-3">
      <div className="rounded-lg border bg-background">
        <textarea
          ref={textareaRef}
          value={value}
          onChange={(e) => {
            setValue(e.target.value);
            handleInput();
          }}
          onKeyDown={handleKeyDown}
          placeholder={disabled ? "This session is archived" : "Ask Multica..."}
          disabled={isRunning || disabled}
          className="block w-full resize-none bg-transparent px-3 pt-3 pb-2 text-sm placeholder:text-muted-foreground focus:outline-none disabled:opacity-50"
          rows={1}
        />
        <div className="flex items-center justify-end px-2 pb-2">
          {isRunning ? (
            <button
              onClick={onStop}
              className="flex size-7 items-center justify-center rounded-full bg-foreground text-background transition-opacity hover:opacity-80"
            >
              <Square className="size-3 fill-current" />
            </button>
          ) : (
            <button
              onClick={handleSend}
              disabled={!value.trim() || disabled}
              className="flex size-7 items-center justify-center rounded-full bg-foreground text-background transition-opacity hover:opacity-80 disabled:opacity-30"
            >
              <ArrowUp className="size-4" />
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
