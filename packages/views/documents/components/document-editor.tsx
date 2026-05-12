"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

interface DocumentEditorProps {
  content: string;
  onSave: (content: string, force?: boolean) => void;
  disabled?: boolean;
  className?: string;
}

/**
 * Markdown textarea with debounced autosave (900ms) and manual save (Ctrl+S).
 * Ported from the paperclip IssueDocumentsSection pattern.
 */
export function DocumentEditor({
  content,
  onSave,
  disabled,
  className,
}: DocumentEditorProps) {
  const { t } = useT("documents");
  const [draft, setDraft] = useState(content);
  const [saveStatus, setSaveStatus] = useState<"saved" | "saving" | "unsaved">("saved");
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const onSaveRef = useRef(onSave);
  const lastSavedRef = useRef(content);
  onSaveRef.current = onSave;

  // Reset draft when content changes externally (e.g. revision restore)
  useEffect(() => {
    setDraft(content);
    lastSavedRef.current = content;
    setSaveStatus("saved");
  }, [content]);

  const handleManualSave = useCallback(() => {
    if (timerRef.current) clearTimeout(timerRef.current);
    setSaveStatus("saving");
    lastSavedRef.current = draft;
    onSaveRef.current(draft, true); // true = force new revision
    setSaveStatus("saved");
  }, [draft]);

  const scheduleSave = useCallback(
    (value: string) => {
      if (timerRef.current) clearTimeout(timerRef.current);
      setSaveStatus("unsaved");
      timerRef.current = setTimeout(() => {
        // Skip save if content hasn't changed since last save
        if (value === lastSavedRef.current) {
          setSaveStatus("saved");
          return;
        }
        setSaveStatus("saving");
        lastSavedRef.current = value;
        onSaveRef.current(value, false); // false = allow collapsing
        setSaveStatus("saved");
      }, 900);
    },
    [],
  );

  // Keyboard shortcut Ctrl+S
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key === "s") {
        e.preventDefault();
        handleManualSave();
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [handleManualSave]);

  // Cleanup timer on unmount
  useEffect(() => {
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, []);

  const handleChange = (value: string) => {
    setDraft(value);
    scheduleSave(value);
  };

  return (
    <div className={cn("flex flex-1 flex-col", className)}>
      <Textarea
        value={draft}
        onChange={(e) => handleChange(e.target.value)}
        disabled={disabled}
        placeholder={t(($) => $.editor.placeholder)}
        className="flex-1 resize-none rounded-none border-0 font-mono text-sm leading-relaxed focus-visible:ring-0"
      />
      <div className="flex h-9 shrink-0 items-center justify-between border-t px-3">
        <div className="flex items-center gap-2">
          <button
            onClick={handleManualSave}
            disabled={disabled || saveStatus === "saved"}
            className="text-[10px] font-medium text-primary hover:underline disabled:text-muted-foreground/50"
          >
            {t(($) => $.editor.save_now)} {t(($) => $.editor.save_shortcut)}
          </button>
        </div>
        <span
          className={cn(
            "text-[10px]",
            saveStatus === "unsaved"
              ? "text-warning"
              : saveStatus === "saving"
                ? "text-muted-foreground"
                : "text-muted-foreground/50",
          )}
        >
          {saveStatus === "unsaved" && t(($) => $.editor.unsaved)}
          {saveStatus === "saving" && t(($) => $.editor.saving)}
          {saveStatus === "saved" && t(($) => $.editor.saved)}
        </span>
      </div>
    </div>
  );
}
