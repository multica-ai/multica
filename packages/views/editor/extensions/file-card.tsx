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
import { useEffect, useMemo, useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { createLowlight, common } from "lowlight";
// @ts-expect-error -- hast-util-to-html has no bundled type declarations
import { toHtml } from "hast-util-to-html";
import { Eye, Loader2, Download, Trash2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { AttachmentFileIcon, getAttachmentFileKind } from "../attachment-file-icon";

const lowlight = createLowlight(common);

// ---------------------------------------------------------------------------
// Text/code preview helpers
// ---------------------------------------------------------------------------

/** Max bytes to fetch for inline preview — prevents huge files from freezing the UI. */
const TEXT_PREVIEW_MAX_BYTES = 256 * 1024;

/** Map file extension → lowlight/hljs language name. */
const EXTENSION_LANGUAGE_MAP: Record<string, string> = {
  bash: "bash",
  c: "c",
  cc: "cpp",
  cpp: "cpp",
  cs: "csharp",
  css: "css",
  go: "go",
  h: "c",
  hpp: "cpp",
  htm: "xml",
  html: "xml",
  java: "java",
  js: "javascript",
  json: "json",
  jsx: "javascript",
  kt: "kotlin",
  lua: "lua",
  mjs: "javascript",
  php: "php",
  py: "python",
  rb: "ruby",
  rs: "rust",
  scss: "scss",
  sh: "bash",
  sql: "sql",
  swift: "swift",
  toml: "ini",
  ts: "typescript",
  tsx: "typescript",
  xml: "xml",
  yaml: "yaml",
  yml: "yaml",
  zsh: "bash",
};

function extractExtension(href: string, filename: string): string {
  const source = filename || href;
  const path = (() => {
    try { return new URL(source).pathname; } catch { return source.split(/[?#]/)[0] ?? source; }
  })();
  const name = path.split("/").pop() ?? path;
  const dot = name.lastIndexOf(".");
  return dot >= 0 ? name.slice(dot + 1).toLowerCase() : "";
}

function getLanguageHint(href: string, filename: string): string | null {
  const ext = extractExtension(href, filename);
  return EXTENSION_LANGUAGE_MAP[ext] ?? null;
}


// ---------------------------------------------------------------------------
// CDN URL detection
// ---------------------------------------------------------------------------

const IMAGE_EXTS = /\.(png|jpe?g|gif|webp|svg|ico|bmp|tiff?)$/i;
// Uploaded files are keyed as {uuid}.{ext} regardless of CDN/MinIO/S3 domain
const UPLOAD_UUID_RE = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.[a-z0-9]+$/i;

/** Check if a URL points to an uploaded file (any storage backend). */
export function isCdnUrl(url: string): boolean {
  try {
    const u = new URL(url);
    return (
      UPLOAD_UUID_RE.test(u.pathname) ||
      u.hostname.endsWith(".copilothub.ai") ||
      u.hostname.endsWith(".amazonaws.com")
    );
  } catch {
    return false;
  }
}

/** Check if an uploaded URL is a non-image file that should render as a file card. */
export function isFileCardUrl(url: string): boolean {
  return isCdnUrl(url) && !IMAGE_EXTS.test(new URL(url).pathname);
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Shared React view
// ---------------------------------------------------------------------------

/** Fetch text content (truncated) when preview is open. */
function useTextPreview(href: string, isOpen: boolean) {
  const [content, setContent] = useState("");
  const [truncated, setTruncated] = useState(false);
  const [error, setError] = useState(false);

  useEffect(() => {
    if (!href || !isOpen) return;
    let cancelled = false;
    setContent("");
    setTruncated(false);
    setError(false);
    fetch(href)
      .then((res) => {
        if (!res.ok) throw new Error("failed");
        return res.text();
      })
      .then((text) => {
        if (cancelled) return;
        if (text.length > TEXT_PREVIEW_MAX_BYTES) {
          setContent(text.slice(0, TEXT_PREVIEW_MAX_BYTES));
          setTruncated(true);
        } else {
          setContent(text);
        }
      })
      .catch(() => {
        if (!cancelled) setError(true);
      });
    return () => { cancelled = true; };
  }, [href, isOpen]);

  return { content, truncated, error };
}

function MarkdownFilePreview({
  href,
  filename,
  editable,
  onDelete,
}: {
  href: string;
  filename: string;
  editable: boolean;
  onDelete?: () => void;
}) {
  const [isPreviewOpen, setIsPreviewOpen] = useState(false);
  const { content, error } = useTextPreview(href, isPreviewOpen);

  return (
    <div className="attachment-card-shell" contentEditable={false}>
      <div className="attachment-card-header flex items-center gap-2 px-3 py-1 transition-colors hover:bg-muted">
        <div className="attachment-title flex-1">
          {editable && (
            <span className="attachment-drag-handle" data-drag-handle aria-hidden="true" />
          )}
          <AttachmentFileIcon href={href} filename={filename} className="size-3" />
          <p className="truncate text-xs">{filename || "Markdown"}</p>
        </div>
        <div className="attachment-actions">
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="attachment-action-button"
            onMouseDown={(e) => {
              e.preventDefault();
              e.stopPropagation();
              setIsPreviewOpen((open) => !open);
            }}
            title={isPreviewOpen ? "Hide markdown preview" : "Show markdown preview"}
          >
            <Eye className="size-3.5" />
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="attachment-action-button"
            onMouseDown={(e) => {
              e.preventDefault();
              e.stopPropagation();
              window.open(href, "_blank", "noopener,noreferrer");
            }}
            title="Download markdown"
          >
            <Download className="size-3.5" />
          </Button>
          {editable && (
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="attachment-action-button attachment-action-button-destructive"
              onMouseDown={(e) => {
                e.preventDefault();
                e.stopPropagation();
                onDelete?.();
              }}
              title="Delete markdown attachment"
            >
              <Trash2 className="size-3.5" />
            </Button>
          )}
        </div>
      </div>
      {isPreviewOpen && error && (
        <div className="border-t border-border/60 attachment-preview-error">
          Unable to load markdown preview.
        </div>
      )}
      {isPreviewOpen && !error && !content && (
        <div className="border-t border-border/60 attachment-preview-loading">
          Loading markdown preview...
        </div>
      )}
      {isPreviewOpen && !error && content && (
        <div className="border-t border-border/60 markdown-file-preview-content">
          <ReactMarkdown remarkPlugins={[[remarkGfm, { singleTilde: false }]]}>
            {content}
          </ReactMarkdown>
        </div>
      )}
    </div>
  );
}

function TextFilePreview({
  href,
  filename,
  kind,
  editable,
  onDelete,
}: {
  href: string;
  filename: string;
  kind: "code" | "text";
  editable: boolean;
  onDelete?: () => void;
}) {
  const [isPreviewOpen, setIsPreviewOpen] = useState(false);
  const { content, truncated, error } = useTextPreview(href, isPreviewOpen);
  const language = useMemo(() => getLanguageHint(href, filename), [href, filename]);

  const highlightedHtml = useMemo(() => {
    if (kind !== "code" || !content) return null;
    try {
      const tree = language && lowlight.listLanguages().includes(language)
        ? lowlight.highlight(language, content)
        : lowlight.highlightAuto(content);
      return toHtml(tree);
    } catch {
      return null;
    }
  }, [kind, content, language]);

  return (
    <div className="attachment-card-shell" contentEditable={false}>
      <div className="attachment-card-header flex items-center gap-2 px-3 py-1 transition-colors hover:bg-muted">
        <div className="attachment-title flex-1">
          {editable && (
            <span className="attachment-drag-handle" data-drag-handle aria-hidden="true" />
          )}
          <AttachmentFileIcon href={href} filename={filename} className="size-3" />
          <p className="truncate text-xs">{filename || (kind === "code" ? "Code file" : "Text file")}</p>
        </div>
        <div className="attachment-actions">
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="attachment-action-button"
            onMouseDown={(e) => {
              e.preventDefault();
              e.stopPropagation();
              setIsPreviewOpen((open) => !open);
            }}
            title={isPreviewOpen ? "Hide preview" : "Show preview"}
          >
            <Eye className="size-3.5" />
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="attachment-action-button"
            onMouseDown={(e) => {
              e.preventDefault();
              e.stopPropagation();
              window.open(href, "_blank", "noopener,noreferrer");
            }}
            title="Download"
          >
            <Download className="size-3.5" />
          </Button>
          {editable && (
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="attachment-action-button attachment-action-button-destructive"
              onMouseDown={(e) => {
                e.preventDefault();
                e.stopPropagation();
                onDelete?.();
              }}
              title="Delete attachment"
            >
              <Trash2 className="size-3.5" />
            </Button>
          )}
        </div>
      </div>
      {isPreviewOpen && error && (
        <div className="border-t border-border/60 attachment-preview-error">
          Unable to load preview.
        </div>
      )}
      {isPreviewOpen && !error && !content && (
        <div className="border-t border-border/60 attachment-preview-loading">
          Loading preview...
        </div>
      )}
      {isPreviewOpen && !error && content && (
        <div className="border-t border-border/60 text-file-preview-content">
          <pre className={cn("text-file-preview-pre", kind === "code" && language && `language-${language}`)}>
            {kind === "code" && highlightedHtml ? (
              <code
                className={cn("hljs", language && `language-${language}`)}
                dangerouslySetInnerHTML={{ __html: highlightedHtml }}
              />
            ) : (
              <code>{content}</code>
            )}
          </pre>
          {truncated && (
            <div className="text-file-preview-truncated">
              Preview truncated — file exceeds {Math.round(TEXT_PREVIEW_MAX_BYTES / 1024)} KB. Download to see full content.
            </div>
          )}
        </div>
      )}
    </div>
  );
}

export function AttachmentCard({
  href,
  filename,
  uploading = false,
  editable = false,
  onDelete,
}: {
  href: string;
  filename: string;
  uploading?: boolean;
  editable?: boolean;
  onDelete?: () => void;
}) {
  const kind = getAttachmentFileKind(href, filename);
  const [isPdfPreviewOpen, setIsPdfPreviewOpen] = useState(false);

  const openFile = () => {
    window.open(href, "_blank", "noopener,noreferrer");
  };

  if (!uploading && kind === "markdown" && href) {
    return (
      <div className="file-card-node" data-type="fileCard" contentEditable={false}>
        <MarkdownFilePreview href={href} filename={filename} editable={editable} onDelete={onDelete} />
      </div>
    );
  }

  if (!uploading && (kind === "code" || kind === "text") && href) {
    return (
      <div className="file-card-node" data-type="fileCard" contentEditable={false}>
        <TextFilePreview href={href} filename={filename} kind={kind} editable={editable} onDelete={onDelete} />
      </div>
    );
  }

  return (
    <div className="file-card-node attachment-card-shell" data-type="fileCard" contentEditable={false}>
      <div
        className="attachment-card-header flex items-center gap-2 px-3 py-1 transition-colors hover:bg-accent/20"
        contentEditable={false}
      >
        {editable && (
          <span className="attachment-drag-handle" data-drag-handle aria-hidden="true" />
        )}
        {uploading ? (
          <Loader2 className="size-3 shrink-0 animate-spin text-muted-foreground" />
        ) : (
          <AttachmentFileIcon href={href} filename={filename} className="size-3" />
        )}
        <div className="min-w-0 flex-1">
          <p className="truncate text-xs">{uploading ? `Uploading ${filename}` : filename}</p>
        </div>
        <div className="attachment-actions">
          {!uploading && kind === "pdf" && href && (
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="attachment-action-button"
              onMouseDown={(e) => {
                e.preventDefault();
                e.stopPropagation();
                setIsPdfPreviewOpen((open) => !open);
              }}
              title={isPdfPreviewOpen ? "Hide PDF preview" : "Show PDF preview"}
            >
              <Eye className="size-3.5" />
            </Button>
          )}
          {!uploading && href && (
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="attachment-action-button"
              onMouseDown={(e) => {
                e.preventDefault();
                e.stopPropagation();
                openFile();
              }}
              title="Download attachment"
            >
              <Download className="size-3.5" />
            </Button>
          )}
          {editable && (
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="attachment-action-button attachment-action-button-destructive"
              onMouseDown={(e) => {
                e.preventDefault();
                e.stopPropagation();
                onDelete?.();
              }}
              title="Delete attachment"
            >
              <Trash2 className="size-3.5" />
            </Button>
          )}
        </div>
      </div>

      {!uploading && kind === "pdf" && href && isPdfPreviewOpen && (
        <div className="border-t border-border/60 pdf-file-preview">
          <iframe src={`${href}#toolbar=0&navpanes=0`} title={filename || "PDF preview"} />
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// React NodeView
// ---------------------------------------------------------------------------

function FileCardView({ node, editor, deleteNode }: NodeViewProps) {
  const href = (node.attrs.href as string) || "";
  const filename = (node.attrs.filename as string) || "";
  const uploading = node.attrs.uploading as boolean;

  return (
    <NodeViewWrapper as="div" className="file-card-node-wrapper" data-type="fileCard">
      <AttachmentCard
        href={href}
        filename={filename}
        uploading={uploading}
        editable={editor.isEditable}
        onDelete={deleteNode}
      />
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
  draggable: true,

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
