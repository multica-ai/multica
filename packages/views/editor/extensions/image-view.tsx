"use client";

/**
 * ImageView — Tiptap NodeView for the image node.
 *
 * Thin wrapper around the unified `<Attachment>` dispatcher. All rendering
 * (figure, hover toolbar, lightbox/preview) lives there. The NodeView only
 * forwards Tiptap's editor-context hints (editable, selected, deleteNode) and
 * keeps a click-to-view from leaving the image selected.
 */

import { NodeViewWrapper } from "@tiptap/react";
import type { NodeViewProps } from "@tiptap/react";
import { Selection } from "@tiptap/pm/state";
import { Attachment } from "../attachment";

function ImageView({ node, editor, selected, deleteNode, getPos }: NodeViewProps) {
  const src = (node.attrs.src as string) || "";
  const alt = (node.attrs.alt as string) || "";
  const uploading = node.attrs.uploading as boolean;
  const width = (node.attrs.width as number | null) ?? undefined;
  const height = (node.attrs.height as number | null) ?? undefined;

  // A click on the image opens the viewer, but ProseMirror has already
  // node-selected it by then. Left selected, the first keystroke after the
  // viewer hands focus back would replace the image, so the caret moves to
  // the nearest text instead.
  const releaseSelection = () => {
    const pos = getPos();
    if (typeof pos !== "number") return;
    const { state, view } = editor;
    const $after = state.doc.resolve(pos + node.nodeSize);
    const next =
      Selection.findFrom($after, 1, true) ?? Selection.findFrom($after, -1, true);
    if (next) view.dispatch(state.tr.setSelection(next));
  };

  // <Attachment> emits its own .image-node wrapper, so the NodeViewWrapper
  // stays unclassed — no double image-node.
  return (
    <NodeViewWrapper
      onClick={editor.isEditable && !uploading && src ? releaseSelection : undefined}
    >
      <Attachment
        attachment={{
          kind: "url",
          url: src,
          filename: alt,
          uploading,
          // Intrinsic dimensions reserve the <img> box pre-decode (no shift).
          width,
          height,
          // Tiptap image node is structurally an image regardless of alt.
          forceKind: "image",
        }}
        editable={editor.isEditable}
        selected={selected}
        onDelete={() => deleteNode()}
      />
    </NodeViewWrapper>
  );
}

export { ImageView };
