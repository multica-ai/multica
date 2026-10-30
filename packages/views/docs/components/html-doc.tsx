"use client";

import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

export type HtmlDocMode = "preview" | "source";

export interface HtmlDocHandle {
  read: () => string;
  adopt: (text: string) => void;
}

const SAVE_DELAY_MS = 1500;

/**
 * A full-page HTML doc. The page runs in a sandboxed iframe with an opaque
 * origin (scripts allowed, no same-origin), the same policy as HTML
 * attachment previews; the source is a plain text editor.
 */
export const HtmlDoc = forwardRef<
  HtmlDocHandle,
  { initial: string; title: string; mode: HtmlDocMode; onChange: (text: string) => void }
>(function HtmlDoc({ initial, title, mode, onChange }, ref) {
  const { t } = useT("docs");
  const [text, setText] = useState(initial);
  const textRef = useRef(initial);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;
  // The preview re-renders after typing pauses, not on every keystroke.
  const [preview, setPreview] = useState(initial);

  useImperativeHandle(ref, () => ({
    read: () => textRef.current,
    adopt: (next: string) => {
      clearTimeout(timer.current);
      timer.current = undefined;
      textRef.current = next;
      setText(next);
      setPreview(next);
    },
  }));

  useEffect(() => {
    if (mode === "preview") setPreview(textRef.current);
  }, [mode]);

  useEffect(
    () => () => {
      if (timer.current) {
        clearTimeout(timer.current);
        onChangeRef.current(textRef.current);
      }
    },
    [],
  );

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {mode === "preview" ? (
        <iframe
          title={title}
          srcDoc={preview}
          sandbox="allow-scripts"
          referrerPolicy="no-referrer"
          className="min-h-0 w-full flex-1 border-0 bg-white"
        />
      ) : (
        <textarea
          value={text}
          spellCheck={false}
          aria-label={t(($) => $.html.source_label)}
          onChange={(event) => {
            const next = event.target.value;
            textRef.current = next;
            setText(next);
            clearTimeout(timer.current);
            timer.current = setTimeout(() => {
              timer.current = undefined;
              setPreview(textRef.current);
              onChangeRef.current(textRef.current);
            }, SAVE_DELAY_MS);
          }}
          className={cn(
            "min-h-0 w-full flex-1 resize-none bg-transparent px-6 py-4 font-mono text-caption leading-relaxed text-foreground outline-none",
          )}
        />
      )}
    </div>
  );
});
