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
import { copyText } from "@multica/ui/lib/clipboard";
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
       * Intrinsic pixel dimensions. Rendered as `<img width height>` so the
       * browser reserves the box before the image decodes — prevents the
       * layout shift that would otherwise push the caret out of view on paste.
       */
      width?: number;
      height?: number;
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
  width?: number;
  height?: number;
}

function normalize(
  input: AttachmentInput,
  resolve: (url: string) => AttachmentRecord | undefined,
): Normalized {
  if (input.kind === "record") {
    return {
      filename: input.attachment.filename,
      contentType: input.attachment.content_type,
      url: pickInlineMediaURL(input.attachment, input.attachment.url),
      attachmentId: input.attachment.id,
      record: input.attachment,
      uploading: false,
    };
  }
  const record = input.url ? resolve(input.url) : undefined;
  return {
    filename: input.filename || record?.filename || "",
    contentType: input.contentType || record?.content_type || "",
    // When the markdown URL resolved to an attachment record, swap to
    // the record's freshly-loadable URL. The persisted markdown URL
    // (`/api/attachments/<id>/download` for new content; raw stored URL
    // for legacy) is correct as a stable reference but doesn't
    // necessarily load as a native <img>/<video> resource for every
    // client — token-mode clients can't attach an Authorization header
    // to bare /api/* fetches, and a CloudFront-signed `download_url`
    // is the only working media src in that mode. `pickInlineMediaURL`
    // picks the URL with embedded credentials when one exists and
    // falls back to the input URL otherwise so legacy / unresolved
    // markdown stays on its existing path. See MUL-3130 review.
    url: record ? pickInlineMediaURL(record, input.url) : input.url,
    attachmentId: record?.id,
    record,
    uploading: !!input.uploading,
    width: input.width,
    height: input.height,
  };
}

// pickInlineMediaURL returns the URL most likely to load successfully
// inside a native <img>/<video>/<iframe> resource fetch — i.e. without
// the calling client attaching an Authorization header.
//
// The metadata response from the backend offers two URL fields per
// attachment row:
//
//   - `record.url`         — for LocalStorage this is a freshly-signed
//                             `/uploads/<key>?exp=<unix>&sig=<HMAC>`
//                             URL whose query string IS the auth (works
//                             for token-mode <img> loads). For S3/
//                             CloudFront it's the raw stored URL with
//                             no signature.
//   - `record.download_url` — `/api/attachments/<id>/download` in the
//                             default proxy/presign mode (requires
//                             cookie or Authorization header — does
//                             NOT work as a native resource load for
//                             token-mode clients). In CloudFront mode
//                             this is replaced server-side with a
//                             CloudFront-signed URL that DOES work as
//                             a native <img> src.
//
// Heuristic: when `download_url` is an absolute URL with a recognised
// CDN signature query (`Signature` / `Expires` / `Key-Pair-Id` for
// CloudFront, `X-Amz-Signature` / `X-Amz-Expires` for raw S3 presigns
// that may surface here in future modes), use it. Otherwise use
// `record.url`, which carries the LocalStorage `?exp&sig` token and is
// the only inline-loadable URL in that backend. Falls back to the
// input URL when neither is usable so legacy markdown links keep their
// pre-fix behaviour.
function pickInlineMediaURL(record: AttachmentRecord, fallback: string): string {
  const dl = record.download_url ?? "";
  if (
    /^https?:\/\//i.test(dl) &&
    /[?&](Signature|X-Amz-Signature|Key-Pair-Id|Expires|X-Amz-Expires)=/i.test(dl)
  ) {
    return dl;
  }
  if (record.url) return record.url;
  return fallback;
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
          width={state.width}
          height={state.height}
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
  width?: number;
  height?: number;
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
  width,
  height,
  editable,
  selected,
  onView,
  onDownload,
  onDelete,
  className,
}: ImageAttachmentViewProps) {
  const { t } = useT("editor");

  const handleCopyLink = async () => {
    if (await copyText(src)) {
      toast.success(t(($) => $.image.link_copied));
    } else {
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
          width={width}
          height={height}
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
