"use client";

import * as React from "react";

export interface SelectionButtonState {
  /** The trimmed selected text. */
  text: string;
  /** Viewport coordinates of the selection's top edge, horizontally centred. */
  top: number;
  left: number;
}

// useEditorSelectionButton watches the window selection and, when a non-empty
// selection sits inside `containerRef` (the note editor), returns its text plus
// a viewport position so the caller can float a "comment on this selection"
// button above it — the Google-Docs-style affordance Jesper asked for
// (TECH-3637). Cleared on collapse, on selections outside the editor, or via
// `dismiss()`. Disabled entirely when `active` is false.
export function useEditorSelectionButton(
  containerRef: React.RefObject<HTMLElement | null>,
  active: boolean,
): { selection: SelectionButtonState | null; dismiss: () => void } {
  const [selection, setSelection] = React.useState<SelectionButtonState | null>(
    null,
  );
  const dismiss = React.useCallback(() => setSelection(null), []);

  React.useEffect(() => {
    if (!active) {
      setSelection(null);
      return;
    }
    const handler = () => {
      const container = containerRef.current;
      const sel = typeof window !== "undefined" ? window.getSelection() : null;
      if (!container || !sel || sel.isCollapsed || sel.rangeCount === 0) {
        setSelection(null);
        return;
      }
      const range = sel.getRangeAt(0);
      if (!container.contains(range.commonAncestorContainer)) {
        setSelection(null);
        return;
      }
      const text = sel.toString().trim();
      if (!text) {
        setSelection(null);
        return;
      }
      const rect = range.getBoundingClientRect();
      if (!rect || (rect.width === 0 && rect.height === 0)) {
        setSelection(null);
        return;
      }
      setSelection({ text, top: rect.top, left: rect.left + rect.width / 2 });
    };
    document.addEventListener("selectionchange", handler);
    return () => document.removeEventListener("selectionchange", handler);
  }, [containerRef, active]);

  return { selection, dismiss };
}
