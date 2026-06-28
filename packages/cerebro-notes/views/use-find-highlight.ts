"use client";

// useFindHighlight wires the find-highlight decoration plugin to a TipTap
// ContentEditor instance (obtained via its onEditorReady callback). Mirrors
// useCommentAnchors: registers the plugin once, dispatches state on changes,
// and scrolls the active match into view (FIR-2145).

import * as React from "react";
import type { Editor } from "@tiptap/react";
import { findHighlightPlugin, findHighlightKey } from "./find-highlight-plugin";

export function useFindHighlight(
  editor: Editor | null,
  query: string,
  activeIndex: number,
): void {
  React.useEffect(() => {
    if (!editor) return;
    editor.registerPlugin(findHighlightPlugin());
    return () => {
      if (!editor.isDestroyed) {
        try {
          editor.unregisterPlugin(findHighlightKey);
        } catch {
          // editor already torn down
        }
      }
    };
  }, [editor]);

  React.useEffect(() => {
    if (!editor || editor.isDestroyed) return;
    editor.view.dispatch(
      editor.state.tr.setMeta(findHighlightKey, { query, activeIndex }),
    );
  }, [editor, query, activeIndex]);

  React.useEffect(() => {
    if (!editor || editor.isDestroyed || activeIndex < 0 || !query) return;
    const raf = requestAnimationFrame(() => {
      const el = editor.view.dom.querySelector(".find-highlight-active");
      el?.scrollIntoView({ block: "center", behavior: "smooth" });
    });
    return () => cancelAnimationFrame(raf);
  }, [editor, activeIndex, query]);
}
