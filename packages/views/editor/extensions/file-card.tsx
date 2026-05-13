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
import { Eye, FileText, Loader2, Download } from "lucide-react";
import { useT } from "../../i18n";
import { useAttachmentDownloadResolver } from "../attachment-download-context";
import { useAttachmentPreview } from "../attachment-preview-modal";
import { isPreviewable } from "../utils/preview";


// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// React NodeView
// ---------------------------------------------------------------------------

function FileCardView({ node }: NodeViewProps) {
  const { t } = useT("editor");
  const href = (node.attrs.href as string) || "";
  const filename = (node.attrs.filename as string) || "";
  const uploading = node.attrs.uploading as boolean;
  const { openByUrl, resolveAttachment } = useAttachmentDownloadResolver();
  const preview = useAttachmentPreview();

  const openFile = () => {
    openByUrl(href);
  };

  // The NodeView only holds href + filename. The full Attachment (with
  // content_type / download_url) lives in the surrounding
  // AttachmentDownloadProvider — resolve it lazily at click time so the
  // eye button is only offered when we both know the record and the
  // dispatcher recognizes the type.
  const attachment = href ? resolveAttachment(href) : undefined;
  const previewable = attachment
    ? isPreviewable(attachment.content_type, attachment.filename)
    : false;

  return (
    <NodeViewWrapper as="div" className="file-card-node" data-type="fileCard">
      <div
        className="my-1 flex items-center gap-2 rounded-md border border-border bg-muted/50 px-2.5 py-1 transition-colors hover:bg-muted"
        contentEditable={false}
        onMouseDown={(e) => e.stopPropagation()}
      >
        {uploading ? (
          <Loader2 className="size-4 shrink-0 animate-spin text-muted-foreground" />
        ) : (
          <FileText className="size-4 shrink-0 text-muted-foreground" />
        )}
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm">{uploading ? t(($) => $.file_card.uploading, { filename }) : filename}</p>
        </div>
        {!uploading && href && previewable && attachment && (
          <button
            type="button"
            className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
            title={t(($) => $.attachment.preview)}
            aria-label={t(($) => $.attachment.preview)}
            onMouseDown={(e) => {
              e.preventDefault();
              e.stopPropagation();
              preview.tryOpen(attachment);
            }}
          >
            <Eye className="size-3.5" />
          </button>
        )}
        {!uploading && href && (
          <button
            type="button"
            className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
            title={t(($) => $.image.download)}
            aria-label={t(($) => $.image.download)}
            onMouseDown={(e) => {
              e.preventDefault();
              e.stopPropagation();
              openFile();
            }}
          >
            <Download className="size-3.5" />
          </button>
        )}
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
      const match = src.match(/^!file\[([^\]]*)\]\((https?:\/\/[^)]+)\)/);
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
