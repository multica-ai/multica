"use client";

import * as React from "react";
import type { Editor } from "@tiptap/react";
import {
  commentAnchorPlugin,
  commentAnchorKey,
  type CommentAnchor,
} from "./comment-anchor-plugin";

// useCommentAnchors wires the comment-anchor decoration plugin to a ContentEditor
// instance (obtained via its onEditorReady callback). It:
//   1. registers the plugin once the editor exists (and tears it down on unmount),
//   2. pushes the current anchors + active id into the plugin as a transaction
//      meta whenever they change, so the orange highlight follows the data, and
//   3. scrolls the active anchor into view when it changes (clicking a comment
//      brings its marking back on screen).
export function useCommentAnchors(
  editor: Editor | null,
  anchors: CommentAnchor[],
  activeId: string | null,
): void {
  React.useEffect(() => {
    if (!editor) return;
    editor.registerPlugin(commentAnchorPlugin());
    return () => {
      if (!editor.isDestroyed) {
        try {
          editor.unregisterPlugin(commentAnchorKey);
        } catch {
          // editor already torn down — nothing to unregister.
        }
      }
    };
  }, [editor]);

  React.useEffect(() => {
    if (!editor || editor.isDestroyed) return;
    editor.view.dispatch(
      editor.state.tr.setMeta(commentAnchorKey, { anchors, activeId }),
    );
  }, [editor, anchors, activeId]);

  React.useEffect(() => {
    if (!editor || editor.isDestroyed || !activeId) return;
    const raf = requestAnimationFrame(() => {
      const sel =
        typeof CSS !== "undefined" && CSS.escape
          ? CSS.escape(activeId)
          : activeId.replace(/"/g, '\\"');
      const el = editor.view.dom.querySelector(`[data-anchor-id="${sel}"]`);
      el?.scrollIntoView({ block: "center", behavior: "smooth" });
    });
    return () => cancelAnimationFrame(raf);
  }, [editor, activeId, anchors]);
}
