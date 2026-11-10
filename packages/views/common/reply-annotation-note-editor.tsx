"use client";

import type { ReactNode, Ref } from "react";
import { isImeComposing } from "@multica/core/utils";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../i18n";

export function ReplyAnnotationNoteEditor({ inputRef, note, existing, onChange, onConfirm, onCancel, children }: {
  inputRef: Ref<HTMLTextAreaElement>;
  note: string;
  existing: boolean;
  onChange: (note: string) => void;
  onConfirm: () => void;
  onCancel: () => void;
  children?: ReactNode;
}) {
  const { t } = useT("issues");
  return <div className="flex w-80 max-w-[calc(100vw-26px)] flex-col gap-1">
    <textarea ref={inputRef} value={note} rows={Math.min(4, Math.max(1, note.split("\n").length))}
      aria-label={t(($) => $.reply.annotations.note_label)}
      placeholder={t(($) => $.reply.annotations.note_placeholder)}
      className="min-h-8 max-h-[calc(4lh+0.75rem)] w-full min-w-0 resize-none field-sizing-content rounded-sm bg-transparent px-2 py-1.5 text-body outline-none placeholder:text-muted-foreground"
      onChange={(event) => onChange(event.target.value)}
      onKeyDown={(event) => {
        if (event.key === "Enter" && !isImeComposing(event) &&
          !event.shiftKey && !event.metaKey && !event.ctrlKey && !event.altKey) {
          event.preventDefault(); event.stopPropagation(); onConfirm();
        } else if (event.key === "Escape" && !isImeComposing(event)) {
          event.preventDefault(); event.stopPropagation(); onCancel();
        }
      }} />
    <div className="flex items-center justify-end gap-1 px-1 pb-1">
      {children}
      <Button type="button" size="sm" disabled={!note.trim()}
        aria-label={t(($) => existing ? $.reply.annotations.save : $.reply.annotations.confirm_add)}
        aria-keyshortcuts="Enter"
        onClick={onConfirm}>{t(($) => existing ? $.reply.annotations.save_short : $.reply.annotations.confirm_add_short)}</Button>
    </div>
  </div>;
}
