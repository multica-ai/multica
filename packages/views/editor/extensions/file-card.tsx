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
import { useAttachmentDownloadResolver } from "../attachment-download-context";
import { useAttachmentPreview } from "../attachment-preview-modal";
import { AttachmentCard } from "../attachment-card";

const FILE_CARD_MARKDOWN_RE = new RegExp(
  `^!file\\[([^\\]]*)\\]\\((${FILE_CARD_URL_PATTERN.source})\\)`,
);


// ---------------------------------------------------------------------------
// React NodeView
// ---------------------------------------------------------------------------

function FileCardView({ node }: NodeViewProps) {
  const href = (node.attrs.href as string) || "";
  const filename = (node.attrs.filename as string) || "";
  const uploading = node.attrs.uploading as boolean;
  const { openByUrl, resolveAttachment } = useAttachmentDownloadResolver();
  const preview = useAttachmentPreview();

  // Preview gate widens to "anything that can be downloaded AND whose
  // filename is a previewable type". Media kinds remain previewable when the
  // attachment record isn't reachable (e.g. URL was copy-pasted across
  // comments). Text kinds (markdown / html / text) need the id because the
  // preview proxy is ID-keyed.
  const attachment = href ? resolveAttachment(href) : undefined;

  const openPreview = () => {
    if (attachment) {
      preview.tryOpen({ kind: "full", attachment });
    } else if (href) {
      preview.tryOpen({ kind: "url", url: href, filename });
    }
  };

  return (
    <NodeViewWrapper as="div" className="file-card-node" data-type="fileCard">
      <div contentEditable={false}>
        <AttachmentCard
          filename={filename}
          contentType={attachment?.content_type ?? ""}
          attachmentId={attachment?.id}
          href={href}
          uploading={uploading}
          onPreview={openPreview}
          onDownload={() => openByUrl(href)}
        />
      </div>
      {preview.modal}
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
      return {
        type: "fileCard",
        raw: match[0],
        attributes: { filename: match[1], href: match[2] },
      };
    },
  },
  parseMarkdown: (token: any, helpers: any) => {
    return helpers.createNode("fileCard", token.attributes);
  },
  renderMarkdown: (node: any) => {
    const { href, filename } = node.attrs || {};
    return `!file[${filename || "file"}](${href})`;
  },

  addNodeView() {
    return ReactNodeViewRenderer(FileCardView);
  },
});
