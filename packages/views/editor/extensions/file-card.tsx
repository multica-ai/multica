"use client";

/**
 * FileCard — Tiptap node extension for rendering uploaded non-image files
 * as styled cards instead of plain markdown links.
 *
 * Markdown serialization: `!file[filename](href)` — custom syntax that is
 * unambiguous (standard `[name](url)` is indistinguishable from regular links).
 *
 * Loading pipeline: preprocessFileCards in preprocess.ts converts both the
 * new `!file[name](url)` syntax AND legacy `[name](cdnUrl)` lines into HTML
 * divs BEFORE @tiptap/markdown parses the content. The markdownTokenizer
 * below acts as a fallback for any direct markdown parsing that bypasses
 * preprocessing.
 */

import { Node, mergeAttributes } from "@tiptap/core";
import { ReactNodeViewRenderer, NodeViewWrapper } from "@tiptap/react";
import type { NodeViewProps } from "@tiptap/react";
import { FILE_CARD_URL_PATTERN } from "@multica/ui/markdown";
import { escapeMarkdownLabel } from "../utils/escape-markdown-label";
import { Attachment } from "../attachment";

// Backslash is excluded from the label char class so "\x" runs can only be
// consumed by \\. — overlapping alternatives backtrack in 2^n ways (ReDoS,
// GitHub #4881).
const FILE_CARD_MARKDOWN_RE = new RegExp(
  `^!file\\[((?:\\\\.|[^\\]\\\\])*)\\]\\((${FILE_CARD_URL_PATTERN.source})\\)`,
);


// ---------------------------------------------------------------------------
// React NodeView — thin wrapper, all rendering lives in <Attachment>
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Tiptap Node Extension
// ---------------------------------------------------------------------------

export const FileCardExtension = Node.create({
  name: "fileCard",
  group: "block",
  atom: true,

  addAttributes() {
    return {
      href: {
        default: "",
        rendered: false, // Don't put href on DOM — prevents link behavior
      },
      filename: {
        default: "",
        rendered: false,
      },
      fileSize: {
        default: 0,
        rendered: false,
      },
      uploading: {
        default: false,
        rendered: false,
      },
      uploadId: {
        default: null,
        rendered: false,
      },
    };
  },

  parseHTML() {
    return [
      {
        tag: 'div[data-type="fileCard"]',
        getAttrs: (el) => ({
          href: (el as HTMLElement).getAttribute("data-href"),
          filename: (el as HTMLElement).getAttribute("data-filename"),
        }),
      },
    ];
  },

  renderHTML({ node, HTMLAttributes }) {
    return [
      "div",
      mergeAttributes(HTMLAttributes, {
        "data-type": "fileCard",
        "data-href": node.attrs.href,
        "data-filename": node.attrs.filename,
      }),
    ];
  },

  // Markdown: custom !file[name](url) syntax for unambiguous roundtrip.
  // Standard [name](url) is indistinguishable from regular links — the old
  // regex-based CDN hostname matching in preprocessFileCards was fragile.
  markdownTokenizer: {
    name: "fileCard",
    level: "block" as const,
    start(src: string) {
      return src.search(/^!file\[/m);
    },
    tokenize(src: string) {
      const match = src.match(FILE_CARD_MARKDOWN_RE);
      if (!match) return undefined;
      const filename = (match[1] ?? "").replace(/\\([[\]\\()])/g, "$1");
      return {
        type: "fileCard",
        raw: match[0],
        attributes: { filename, href: match[2] },
      };
    },
  },
  parseMarkdown: (token: any, helpers: any) => {
    return helpers.createNode("fileCard", token.attributes);
  },
  renderMarkdown: (node: any) => {
    const { href, filename } = node.attrs || {};
    return `!file[${escapeMarkdownLabel(filename || "file")}](${href})`;
  },

  addNodeView() {
    return ReactNodeViewRenderer(FileCardView);
  },
});
