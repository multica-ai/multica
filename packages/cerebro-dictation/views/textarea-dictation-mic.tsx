"use client";

import { useCallback, type RefObject } from "react";
import { EditorDictationMic } from "./editor-dictation-mic";
import { insertAtCaret } from "../insert-at-caret";

export interface TextareaDictationMicProps {
  /**
   * Ref to the host <textarea>. The finished transcription is inserted at its
   * caret. The textarea component must forward its ref to the DOM node (React
   * 19 ref-as-prop is enough for `@multica/ui` Textarea).
   */
  textareaRef: RefObject<HTMLTextAreaElement | null>;
  disabled?: boolean;
  /** Positioning hook — defaults to a bottom-right corner overlay. */
  className?: string;
}

/**
 * Dictation mic for plain-markdown <textarea> editors (documents, notes) that
 * don't use the rich ContentEditor. Mirrors the rich-editor mic: flag-gated
 * (visibility lives in MicButton via `cerebro_voice_dictation_enabled`),
 * one-shot (record → POST → text), and inserts the result at the textarea's
 * caret via insertAtCaret — which dispatches a native `input` event so a
 * controlled React value updates instead of rendering stale state.
 *
 * Render it inside a `relative` wrapper around the textarea so the default
 * corner positioning lands over the field.
 */
export function TextareaDictationMic({
  textareaRef,
  disabled,
  className,
}: TextareaDictationMicProps) {
  const onTranscribed = useCallback(
    (text: string) => {
      const el = textareaRef.current;
      if (!el) return;
      // Restore focus the mic button stole; the textarea keeps its caret
      // position across blur, so insertAtCaret lands where the user left off.
      el.focus();
      insertAtCaret(el, text);
    },
    [textareaRef],
  );

  return (
    <EditorDictationMic
      disabled={disabled}
      onTranscribed={onTranscribed}
      className={className ?? "absolute bottom-1.5 right-1.5 z-10"}
    />
  );
}
