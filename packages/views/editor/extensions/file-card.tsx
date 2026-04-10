"use client";

/**
 * FileCard — Tiptap node extension for rendering uploaded non-image files
 * as styled cards instead of plain markdown links.
 *
 * Markdown serialization: `[filename](href)` — standard link syntax.
 * Preprocessing in preprocess.ts converts standalone CDN file links back
 * to fileCard HTML on load, completing the roundtrip.
 */

import { Node, mergeAttributes } from "@tiptap/core";
import { ReactNodeViewRenderer, NodeViewWrapper } from "@tiptap/react";
import type { NodeViewProps } from "@tiptap/react";
import { FileText, Loader2, Download } from "lucide-react";


// ---------------------------------------------------------------------------
// CDN URL detection
// ---------------------------------------------------------------------------

const IMAGE_EXTS = /\.(png|jpe?g|gif|webp|svg|ico|bmp|tiff?)$/i;

/** Check if a URL points to our upload CDN (CloudFront or S3 bucket). */
export function isCdnUrl(url: string): boolean {
  try {
    const u = new URL(url);
    return (
      u.hostname.endsWith(".copilothub.ai") ||
      u.hostname.endsWith(".amazonaws.com")
    );
  } catch {
    return false;
  }
}

/** Check if a CDN URL is a non-image file that should render as a file card. */
export function isFileCardUrl(url: string): boolean {
  return isCdnUrl(url) && !IMAGE_EXTS.test(new URL(url).pathname);
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// React NodeView
// ---------------------------------------------------------------------------

function FileCardView({ node }: NodeViewProps) {
  const href = (node.attrs.href as string) || "";
  const filename = (node.attrs.filename as string) || "";
  const uploading = node.attrs.uploading as boolean;

  const openFile = () => {
    window.open(href, "_blank", "noopener,noreferrer");
  };

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
          <p className="truncate text-sm">{uploading ? `Uploading ${filename}` : filename}</p>
        </div>
        {!uploading && href && (
          <button
            type="button"
            className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
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

  // Markdown serialization: fileCard → [filename](href)
  renderMarkdown: (node: any) => {
    const { href, filename } = node.attrs || {};
    return `[${filename || "file"}](${href})`;
  },

  addNodeView() {
    return ReactNodeViewRenderer(FileCardView);
  },
});
