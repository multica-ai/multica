"use client";

/**
 * ImageView — Tiptap NodeView for the image node.
 *
 * Thin wrapper around the unified `<Attachment>` dispatcher. All rendering
 * (figure, hover toolbar, lightbox/preview) lives there. The NodeView only
 * forwards Tiptap's editor-context hints (editable, selected, deleteNode).
 */

import { NodeViewWrapper } from "@tiptap/react";
import type { NodeViewProps } from "@tiptap/react";
import { Attachment } from "../attachment";

function ImageView({ node, editor, selected, deleteNode }: NodeViewProps) {
  const src = (node.attrs.src as string) || "";
  const alt = (node.attrs.alt as string) || "";
  const uploading = node.attrs.uploading as boolean;

  // <Attachment> emits its own .image-node wrapper, so the NodeViewWrapper
  // stays unclassed — no double image-node.
  return (
    <NodeViewWrapper>
      <Attachment
        attachment={{
          kind: "url",
          url: src,
          filename: alt,
          uploading,
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
