"use client";

/**
 * Attachment — single unified renderer for every attachment surface.
 *
 * Takes one attachment-shaped input (a full record, a URL-only reference, or
 * an in-flight upload) and dispatches by PreviewKind:
 *
 *   - image  → ImageAttachmentView (figure + hover toolbar + lightbox via
 *              the shared AttachmentPreviewModal)
 *   - html   → HtmlAttachmentPreview (inline iframe + hover toolbar)
 *   - others → AttachmentCard (icon + filename + Eye/Download row)
 *
 * Call sites:
 *   - extensions/file-card.tsx FileCardView (Tiptap NodeView)
 *   - extensions/image-view.tsx ImageView (Tiptap NodeView)
 *   - readonly-content.tsx (markdown img + fileCard div renderers)
 *   - issues/components/comment-card.tsx AttachmentList (standalone fallback)
 *   - common/markdown.tsx (chat / skill viewer Markdown wrapper)
 *
 * The component owns its own preview modal and download dispatcher — callers
 * just pass `attachment` and (for editor surfaces) optional editor chrome
 * hints (selected, editable, onDelete).
 */

import {
  Download,
  Link as LinkIcon,
  Maximize2,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import { cn } from "@multica/ui/lib/utils";
import type { Attachment as AttachmentRecord } from "@multica/core/types";
import { useT } from "../i18n";
import { useAttachmentDownloadResolver } from "./attachment-download-context";
import { useAttachmentPreview } from "./attachment-preview-modal";
import { useDownloadAttachment } from "./use-download-attachment";
import { AttachmentCard } from "./attachment-card";
import { HtmlAttachmentPreview } from "./html-attachment-preview";
import { getPreviewKind, type PreviewKind } from "./utils/preview";
import "./styles/attachment.css";

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

export type AttachmentInput =
  // Server response in hand — full record. Used by AttachmentList and any
  // caller iterating a server-returned attachments[] array.
  | { kind: "record"; attachment: AttachmentRecord }
  // Markdown / Tiptap inline: only a URL + filename. Resolves to a full
  // record via the surrounding AttachmentDownloadProvider when available;
  // otherwise renders in URL-only mode (media types still preview from URL,
  // text types fall back to a download CTA).
  | {
      kind: "url";
      url: string;
      filename: string;
      contentType?: string;
      /** Editor in-flight state. Renders a loader placeholder. */
      uploading?: boolean;
      /**
       * Structural hint from the call site: "this slot is definitionally an
       * image / file / ...". Bypasses `getPreviewKind` autodetect, which
       * needs a filename or content-type and falls back to the file-card
       * chrome when neither is available. Required for callers that KNOW
       * the kind from context (markdown `![]()` is always an image; Tiptap
       * image NodeView is always an image) but receive only a URL with an
       * empty `alt`/`filename`.
       */
      forceKind?: PreviewKind;
    };

export interface AttachmentProps {
  attachment: AttachmentInput;
  /** Editor hint — when true, the image toolbar exposes Trash. */
  editable?: boolean;
  /** Editor hint — applies the "selected" visual to the image figure. */
  selected?: boolean;
  /** Editor hint — wired to Tiptap deleteNode(). */
  onDelete?: () => void;
  className?: string;
}

interface Normalized {
  filename: string;
  contentType: string;
  url: string;
  attachmentId?: string;
  record?: AttachmentRecord;
  uploading: boolean;
}

function normalize(
  input: AttachmentInput,
  resolve: (url: string) => AttachmentRecord | undefined,
): Normalized {
  if (input.kind === "record") {
    return {
      filename: input.attachment.filename,
      contentType: input.attachment.content_type,
      url: input.attachment.url,
      attachmentId: input.attachment.id,
      record: input.attachment,
      uploading: false,
    };
  }
  const record = input.url ? resolve(input.url) : undefined;
  return {
    filename: input.filename || record?.filename || "",
    contentType: input.contentType || record?.content_type || "",
    url: input.url,
    attachmentId: record?.id,
    record,
    uploading: !!input.uploading,
  };
}

// ---------------------------------------------------------------------------
// Dispatcher
// ---------------------------------------------------------------------------

export function Attachment({
  attachment,
  editable,
  selected,
  onDelete,
  className,
}: AttachmentProps) {
  const { resolveAttachment, openByUrl } = useAttachmentDownloadResolver();
  const download = useDownloadAttachment();
  const preview = useAttachmentPreview();

  const state = normalize(attachment, resolveAttachment);
  const forceKind =
    attachment.kind === "url" ? attachment.forceKind : undefined;
  const kind =
    forceKind ??
    (state.filename || state.contentType
      ? getPreviewKind(state.contentType, state.filename)
      : null);

  const openPreview = () => {
    if (state.record) {
      preview.tryOpen({ kind: "full", attachment: state.record });
      return;
    }
    if (state.url) {
      preview.tryOpen({
        kind: "url",
        url: state.url,
        filename: state.filename,
      });
    }
  };

  const handleDownload = () => {
    if (state.attachmentId) {
      download(state.attachmentId);
      return;
    }
    if (state.url) openByUrl(state.url);
  };

  if (kind === "image") {
    return (
      <>
        <ImageAttachmentView
          src={state.url}
          alt={state.filename}
          uploading={state.uploading}
          editable={editable}
          selected={selected}
          onView={openPreview}
          onDownload={handleDownload}
          onDelete={onDelete}
          className={className}
        />
        {preview.modal}
      </>
    );
  }

  if (kind === "html" && state.attachmentId && !state.uploading) {
    return (
      <>
        <HtmlAttachmentPreview
          attachmentId={state.attachmentId}
          filename={state.filename}
          onPreview={openPreview}
          onDownload={handleDownload}
          onDelete={editable ? onDelete : undefined}
        />
        {preview.modal}
      </>
    );
  }

  return (
    <>
      <AttachmentCard
        filename={state.filename}
        contentType={state.contentType}
        attachmentId={state.attachmentId}
        href={state.url || undefined}
        uploading={state.uploading}
        onPreview={openPreview}
        onDownload={handleDownload}
        onDelete={editable ? onDelete : undefined}
      />
      {preview.modal}
    </>
  );
}

// ---------------------------------------------------------------------------
// ImageAttachmentView — inline image with hover toolbar
// ---------------------------------------------------------------------------
//
// DOM and styling are intentionally a direct port of the original
// extensions/image-view.tsx <figure> structure. Shared visual styles live in
// styles/attachment.css under `.image-figure / .image-content / .image-toolbar`
// so standalone surfaces (chat messages, AttachmentList) get identical visuals
// without depending on the editor stylesheet being imported elsewhere.

interface ImageAttachmentViewProps {
  src: string;
  alt: string;
  uploading: boolean;
  editable?: boolean;
  selected?: boolean;
  onView: () => void;
  onDownload: () => void;
  onDelete?: () => void;
  className?: string;
}

function ImageAttachmentView({
  src,
  alt,
  uploading,
  editable,
  selected,
  onView,
  onDownload,
  onDelete,
  className,
}: ImageAttachmentViewProps) {
  const { t } = useT("editor");

  const handleCopyLink = async () => {
    try {
      await navigator.clipboard.writeText(src);
      toast.success(t(($) => $.image.link_copied));
    } catch {
      toast.error(t(($) => $.image.copy_link_failed));
    }
  };

  // Click on figure opens the preview only in non-editor / non-uploading
  // surfaces — inside the editor we let ProseMirror own the click for
  // selection / cursor placement and route preview through the explicit
  // Maximize button. The CSS rule `.image-figure[data-clickable="true"] {
  // cursor: zoom-in }` keys off this same flag for the cursor affordance.
  const clickable = !editable && !uploading;

  // DOM mirrors the original ReadonlyImage (span-only chain so it stays
  // valid HTML5 when rendered inside a markdown <p>). In editor surfaces
  // the NodeViewWrapper still emits its own outer .image-node div around
  // this — the duplicate `image-node` class is harmless.
  return (
    <span className="image-node">
      <span
        className={cn(
          "image-figure",
          selected && editable && "image-selected",
          className,
        )}
        data-clickable={clickable || undefined}
        contentEditable={false}
        onClick={clickable ? onView : undefined}
      >
        <img
          src={src || undefined}
          alt={alt}
          className={cn("image-content", uploading && "image-uploading")}
          draggable={false}
        />
        {!uploading && src && (
          <span
            className="image-toolbar"
            onMouseDown={(e) => e.stopPropagation()}
            onClick={(e) => e.stopPropagation()}
          >
            <button type="button" onClick={onView} title={t(($) => $.image.view)}>
              <Maximize2 className="size-3.5" />
            </button>
            <button type="button" onClick={onDownload} title={t(($) => $.image.download)}>
              <Download className="size-3.5" />
            </button>
            <button type="button" onClick={handleCopyLink} title={t(($) => $.image.copy_link)}>
              <LinkIcon className="size-3.5" />
            </button>
            {editable && onDelete && (
              <button type="button" onClick={onDelete} title={t(($) => $.image.delete)}>
                <Trash2 className="size-3.5" />
              </button>
            )}
          </span>
        )}
      </span>
    </span>
  );
}
