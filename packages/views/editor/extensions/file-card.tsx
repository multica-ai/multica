"use client";

/**
 * FileCard — Tiptap node extension for rendering uploaded non-image files
 * as styled cards instead of plain markdown links.
 *
 * The schema and Markdown codec live in ../schema/file-card.ts so the
 * collaboration server can load them without React.
 *
 * Loading pipeline: preprocessFileCards in preprocess.ts converts both the
 * new `!file[name](url)` syntax AND legacy `[name](cdnUrl)` lines into HTML
 * divs BEFORE @tiptap/markdown parses the content. The markdownTokenizer
 * acts as a fallback for any direct markdown parsing that bypasses
 * preprocessing.
 */

import { ReactNodeViewRenderer, NodeViewWrapper } from "@tiptap/react";
import type { NodeViewProps } from "@tiptap/react";
import { Attachment } from "../attachment";
import { FileCardSchema } from "../schema/file-card";

// React NodeView — thin wrapper, all rendering lives in <Attachment>

export function FileCardView({ node, editor, deleteNode }: NodeViewProps) {
  const href = (node.attrs.href as string) || "";
  const filename = (node.attrs.filename as string) || "";
  const uploading = node.attrs.uploading as boolean;
  const editable = editor?.isEditable ?? false;

  return (
    <NodeViewWrapper as="div" className="file-card-node" data-type="fileCard">
      <div contentEditable={false}>
        <Attachment
          attachment={{ kind: "url", url: href, filename, uploading }}
          editable={editable}
          onDelete={editable ? deleteNode : undefined}
        />
      </div>
    </NodeViewWrapper>
  );
}

export const FileCardExtension = FileCardSchema.extend({
  addNodeView() {
    return ReactNodeViewRenderer(FileCardView);
  },
});
