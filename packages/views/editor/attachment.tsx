"use client";

/**
 * Attachment — single unified renderer for every attachment surface.
 *
 * Takes one attachment-shaped input (a full record, a URL-only reference, or
 * an in-flight upload) and dispatches by PreviewKind:
 *
 *   - image  → ImageAttachmentView (figure + hover toolbar + lightbox via
 *              the shared AttachmentPreviewModal)
 *   - others → AttachmentCard (icon + filename + Eye/Download row), or
 *              AttachmentFileCard in the card layout
 *
 * An HTML file is a file like any other (MUL-7649): it shows as a card and
 * opens in the viewer. HTML meant to be read in place is written as a
 * ```html block in the body, which renders as a dynamic block.
 *
 * Call sites:
 *   - extensions/file-card.tsx FileCardView (Tiptap NodeView)
 *   - extensions/image-view.tsx ImageView (Tiptap NodeView)
 *   - rich-content/rich-content.tsx (markdown img + fileCard div renderers,
 *     serving Chat, Issue descriptions and Comments through one renderer)
 *   - issues/components/comment-card.tsx AttachmentList (standalone fallback)
 *
 * The component owns its own preview modal and download dispatcher — callers
 * just pass `attachment` and (for editor surfaces) optional editor chrome
 * hints (selected, editable, onDelete).
 */

import {
  Download,
  Loader2,
  Link as LinkIcon,
  Maximize2,
  Trash2,
} from "@multica/ui/icons";
import { useCallback, useState, type CSSProperties, type ReactNode } from "react";
import { toast } from "sonner";
import { cn } from "@multica/ui/lib/utils";
import { copyText } from "@multica/ui/lib/clipboard";
import { api } from "@multica/core/api";
import { useConfigStore } from "@multica/core/config";
import type { Attachment as AttachmentRecord } from "@multica/core/types";
import {
  attachmentDownloadPath,
  attachmentIdFromDownloadURL,
  attachmentIdFromLocalUploadURL,
  attachmentIdFromStorageURL,
} from "@multica/core/types/attachment-url";
import { useT } from "../i18n";
import { DeferredTooltip } from "../common/deferred-tooltip";
import { useAttachmentDownloadResolver } from "./attachment-download-context";
import { useAttachmentPreview } from "./attachment-preview-modal";
import { usePreviewSequence } from "./preview-sequence-context";
import {
  isObjectURL,
  useResignedInlineMedia,
} from "./hooks/use-inline-media-url";
import { useDownloadAttachment } from "./use-download-attachment";
import { AttachmentCard, AttachmentFileCard } from "./attachment-card";
import { useAttachmentDownloadState } from "./hooks/use-attachment-download-state";
import { canOpenPreview, getPreviewKind, type PreviewKind } from "./utils/preview";
import { imageDimensions } from "./utils/image-metadata";
import "./styles/attachment.css";

// Public API

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
  /**
   * "block" (default) renders each kind at full width, as a body renders it.
   * "card" is the form a list of standalone attachments lays out: an image
   * keeps its full size up to a height cap, any other file becomes a card.
   */
  layout?: "block" | "card";
  /** Card layout only — rendered after the file name, e.g. a version badge. */
  badge?: ReactNode;
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

function recordImageFields(
  record: AttachmentRecord | undefined,
): Pick<Normalized, "width" | "height"> {
  if (!record) return {};
  const dims = imageDimensions(record);
  return {
    width: dims?.width,
    height: dims?.height,
  };
}

function normalize(
  input: AttachmentInput,
  resolve: (url: string) => AttachmentRecord | undefined,
  cdnDomain: string,
  cdnSigned: boolean,
): Normalized {
  if (input.kind === "record") {
    return {
      filename: input.attachment.filename,
      contentType: input.attachment.content_type,
      url: absolutizeMediaURL(
        pickInlineMediaURL(input.attachment, input.attachment.url, cdnDomain, cdnSigned),
      ),
      attachmentId: input.attachment.id,
      record: input.attachment,
      uploading: false,
      ...recordImageFields(input.attachment),
    };
  }
  const record = input.url ? resolve(input.url) : undefined;
  const fromRecord = recordImageFields(record);
  const ownSize = input.width !== undefined || input.height !== undefined;
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
    //
    // After picking the credential-bearing URL we run the absolutize
    // pass so a site-relative `/api/attachments/...` or `/uploads/...`
    // path becomes a proper origin-bearing URL when the renderer's
    // document origin doesn't proxy /api or /uploads to the API host
    // (Electron desktop, mobile webview). Web with a same-origin
    // proxy keeps `apiBaseUrl=""` and the helper is a no-op there.
    // See MUL-3192 — quick-create modal regressed because the freshly-
    // uploaded image URL stayed site-relative and Electron's renderer
    // origin (file://) couldn't load it.
    url: absolutizeMediaURL(
      record ? pickInlineMediaURL(record, input.url, cdnDomain, cdnSigned) : input.url,
    ),
    attachmentId: record?.id,
    record,
    uploading: !!input.uploading,
    width: ownSize ? input.width : fromRecord.width,
    height: ownSize ? input.height : fromRecord.height,
  };
}

// absolutizeMediaURL is the legacy-compat fallback for old markdown bodies
// that persisted a site-relative `/api/attachments/<id>/download` or
// `/uploads/<key>` URL.
//
// The current (post-MUL-3192) write path persists an absolute URL chosen
// server-side by `buildMarkdownURL` (see server/internal/handler/file.go),
// so new content already loads natively on every client. This helper only
// matters for content written BEFORE MUL-3192 — those bodies still carry
// the old relative shape, and rendering them on a surface whose document
// origin is NOT the API host (Electron desktop, mobile webview) needs the
// API base URL pinned in at render time.
//
// On web, `api.getBaseUrl()` is empty (the Next.js rewrite proxies /api/*
// to the API host server-side), so this is a no-op there.
//
// http(s)://, blob:, and data: URLs are passed through unchanged — they
// already carry their own origin.
function absolutizeMediaURL(rawUrl: string): string {
  if (!rawUrl) return rawUrl;
  if (/^https?:\/\//i.test(rawUrl)) return rawUrl;
  if (/^blob:/i.test(rawUrl) || /^data:/i.test(rawUrl)) return rawUrl;
  if (!rawUrl.startsWith("/")) return rawUrl;
  // The api singleton is a Proxy that returns `undefined` for any property
  // access before `setApiInstance()` runs (boot ordering, SSR). Optional
  // chaining lets us cope with that without throwing — pre-init renders
  // simply keep the site-relative path.
  const baseUrl = (api.getBaseUrl?.() ?? "").replace(/\/+$/, "");
  if (!baseUrl) return rawUrl;
  return `${baseUrl}${rawUrl}`;
}

// pickInlineMediaURL chooses a URL usable by native media loads, which cannot
// attach the API client's Authorization header. Signed download URLs are suitable
// for rendering but must not replace durable Markdown references.
//
// Use known public/CDN URLs directly only when cdn_signed is false; unsigned URLs
// on a private CDN fail even when the domain matches. Historical local uploads
// use the authenticated API. Otherwise prefer the server's markdown_url,
// then legacy storage URLs, then the original input.
function pickInlineMediaURL(
  record: AttachmentRecord,
  fallback: string,
  cdnDomain: string,
  cdnSigned: boolean,
): string {
  const dl = record.download_url ?? "";
  if (
    /^https?:\/\//i.test(dl) &&
    /[?&](Signature|X-Amz-Signature|Key-Pair-Id|Expires|X-Amz-Expires)=/i.test(dl)
  ) {
    return dl;
  }
  if (attachmentIdFromLocalUploadURL(record.url)) return attachmentDownloadPath(record.id);
  if (!cdnSigned && storageURLMatchesCdnDomain(record.url, cdnDomain)) return record.url;
  if (record.markdown_url) return record.markdown_url;
  if (record.url) return record.url;
  return fallback;
}

function storageURLMatchesCdnDomain(rawURL: string, cdnDomain: string): boolean {
  const expected = normalizeHost(cdnDomain);
  if (!rawURL || !expected) return false;
  try {
    const u = new URL(rawURL);
    if (u.protocol !== "http:" && u.protocol !== "https:") return false;
    if (normalizeHost(u.hostname) !== expected) return false;
    return !hasExpiringSignatureQuery(u.searchParams);
  } catch {
    return false;
  }
}

function normalizeHost(host: string): string {
  return host.trim().toLowerCase().replace(/\.$/, "");
}

function hasExpiringSignatureQuery(q: URLSearchParams): boolean {
  for (const key of [
    "Signature",
    "X-Amz-Signature",
    "Key-Pair-Id",
    "Expires",
    "X-Amz-Expires",
  ]) {
    if (q.has(key)) return true;
  }
  return false;
}

// Dispatcher

export function Attachment({
  attachment,
  editable,
  selected,
  onDelete,
  layout = "block",
  badge,
  className,
}: AttachmentProps) {
  const { resolveAttachment, openByUrl } = useAttachmentDownloadResolver();
  const cdnDomain = useConfigStore((s) => s.cdnDomain);
  const cdnSigned = useConfigStore((s) => s.cdnSigned);
  const download = useDownloadAttachment();
  const preview = useAttachmentPreview();
  const sequence = usePreviewSequence();

  const state = normalize(attachment, resolveAttachment, cdnDomain, cdnSigned);
  const forceKind =
    attachment.kind === "url" ? attachment.forceKind : undefined;
  const kind =
    forceKind ??
    (state.filename || state.contentType
      ? getPreviewKind(state.contentType, state.filename)
      : null);
  // The picked URL may still be the auth-gated API endpoint (reopened drafts
  // whose persisted record has no signed download_url). Upgrade it to a
  // freshly signed URL on clients that can't load the endpoint natively, or —
  // on deployments that have no signed URL to give — to an object URL built
  // from the authenticated byte fetch. Only the image branch renders a native
  // resource load, so only it opts into that byte fetch.
  const { url: mediaUrl } = useResignedInlineMedia(
    state.attachmentId,
    state.url,
    kind === "image",
  );
  // Object URLs are session-local, so anything that hands a URL to the user or
  // to another surface keeps the durable pick instead.
  const shareUrl = isObjectURL(mediaUrl) ? state.url : mediaUrl;

  // Identity this attachment has in the surrounding surface's sequence: the
  // attachment id once the URL resolves to a record, otherwise the URL as the
  // renderer received it — the same pair `collectAttachmentSequence` keys on.
  const sequenceKey =
    state.attachmentId ?? (attachment.kind === "url" ? attachment.url : "");

  const openPreview = () => {
    // Inside an issue / chat, a file opens the surface's shared viewer at its
    // real position so the reader can page through the rest. Anything the
    // sequence doesn't know — a composer's in-flight upload, a surface with no
    // provider — falls through to the single-file preview below.
    if (kind && sequence.openAt(sequenceKey)) return;
    if (state.record) {
      preview.tryOpen({
        kind: "full",
        attachment: {
          ...state.record,
          download_url: mediaUrl || state.record.download_url,
        },
      });
      return;
    }
    if (mediaUrl) {
      preview.tryOpen({
        kind: "url",
        url: isObjectURL(mediaUrl) ? state.url : mediaUrl,
        filename: state.filename,
        // This dispatcher has already decided what the slot is — from the
        // call site's `forceKind`, or from a content-type the modal's
        // URL-only source doesn't carry. Hand that answer over instead of
        // letting the modal re-guess from `filename`, which for a body image
        // is the markdown caption (MUL-7518).
        forceKind: kind ?? undefined,
      });
    }
  };

  const handleDownload = () => {
    // A CDN storage URL that didn't resolve against this surface's
    // attachments (e.g. an image quoted from another comment) still names
    // its attachment id, so re-sign it instead of opening the bare URL,
    // which a signed CDN rejects outside the app.
    const attachmentId =
      state.attachmentId ??
      attachmentIdFromDownloadURL(shareUrl) ??
      attachmentIdFromLocalUploadURL(shareUrl) ??
      attachmentIdFromStorageURL(shareUrl, cdnDomain);
    if (attachmentId) {
      return download(attachmentId);
    }
    if (shareUrl) return openByUrl(shareUrl);
  };

  if (kind === "image") {
    return (
      <>
        <ImageAttachmentView
          src={mediaUrl}
          linkUrl={shareUrl}
          alt={state.filename}
          uploading={state.uploading}
          width={state.width}
          height={state.height}
          editable={editable}
          selected={selected}
          onView={openPreview}
          onDownload={handleDownload}
          onDelete={onDelete}
          className={cn(layout === "card" && "image-standalone", className)}
        />
        {preview.modal}
      </>
    );
  }

  if (layout === "card") {
    return (
      <>
        <AttachmentFileCard
          filename={state.filename}
          contentType={state.contentType}
          sizeBytes={state.record?.size_bytes}
          // Same gate as the row's Eye button: text kinds need the record.
          canPreview={!!shareUrl && canOpenPreview(kind, !!state.attachmentId)}
          canDownload={!!shareUrl || !!state.attachmentId}
          uploading={state.uploading}
          badge={badge}
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
        href={shareUrl || undefined}
        uploading={state.uploading}
        onPreview={openPreview}
        onDownload={handleDownload}
        onDelete={editable ? onDelete : undefined}
      />
      {preview.modal}
    </>
  );
}

// ImageAttachmentView — inline image with hover toolbar
//
// DOM and styling are intentionally a direct port of the original
// extensions/image-view.tsx <figure> structure. Shared visual styles live in
// styles/attachment.css under `.image-figure / .image-content / .image-toolbar`
// so standalone surfaces (chat messages, AttachmentList) get identical visuals
// without depending on the editor stylesheet being imported elsewhere.

interface ImageAttachmentViewProps {
  src: string;
  /**
   * URL handed to the user by Copy Link. Splits from `src` only when `src` is
   * a session-local object URL (proxy-mode byte fallback) — pasting a `blob:`
   * URL anywhere outside this renderer resolves to nothing.
   */
  linkUrl: string;
  alt: string;
  uploading: boolean;
  width?: number;
  height?: number;
  editable?: boolean;
  selected?: boolean;
  onView: () => void;
  onDownload: () => void | Promise<unknown>;
  onDelete?: () => void;
  className?: string;
}

function ImageAttachmentView({
  src,
  linkUrl,
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
  const { downloading, handleDownload } = useAttachmentDownloadState(onDownload);

  const handleCopyLink = async () => {
    if (await copyText(linkUrl)) {
      toast.success(t(($) => $.image.link_copied));
    } else {
      toast.error(t(($) => $.image.copy_link_failed));
    }
  };

  // Click on figure opens the preview on every surface once the upload has
  // settled, editors included: an issue description is always an editor, and
  // its images must open the same way a comment's do. The CSS rule
  // `.image-figure[data-clickable="true"] { cursor: zoom-in }` keys off this
  // same flag for the cursor affordance.
  const clickable = !uploading;

  // An image that has not loaded has no size of its own (the figure shrinks
  // to it), so its box is reserved up front: from its pixel size when known,
  // otherwise as a fixed-ratio placeholder. Once anything has painted the box
  // follows the image.
  const [loadedSrc, setLoadedSrc] = useState<string | null>(null);
  const loaded = !!src && loadedSrc === src;
  const imgRef = useCallback((img: HTMLImageElement | null) => {
    if (img?.complete && img.naturalWidth > 0) setLoadedSrc(src);
  }, [src]);
  const sized = !!width && !!height;
  const pending = !loaded && !sized && !uploading;
  const sizeStyle = sized
    ? ({
        "--image-width": `${width}px`,
        "--image-aspect": `${width! / height!}`,
      } as CSSProperties)
    : undefined;

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
        data-sized={sized || undefined}
        data-pending={pending || undefined}
        style={sizeStyle}
        contentEditable={false}
        onClick={clickable ? onView : undefined}
      >
        <img
          ref={imgRef}
          src={src || undefined}
          alt={alt}
          width={width}
          height={height}
          loading="lazy"
          decoding="async"
          data-loaded={loaded || undefined}
          className={cn("image-content", uploading && "image-uploading")}
          draggable={false}
          onLoad={() => setLoadedSrc(src)}
          onError={() => setLoadedSrc(src)}
        />
        {!uploading && src && (
          <span
            className="image-toolbar"
            style={downloading ? { opacity: 1 } : undefined}
            onMouseDown={(e) => e.stopPropagation()}
            onClick={(e) => e.stopPropagation()}
          >
            <DeferredTooltip
              content={t(($) => $.image.view)}
              trigger={
                <button type="button" onClick={onView} aria-label={t(($) => $.image.view)}>
                  <Maximize2 className="size-3.5" />
                </button>
              }
            />
            <DeferredTooltip
              content={t(($) => $.image.download)}
              trigger={
                <button
                  type="button"
                  onClick={handleDownload}
                  disabled={downloading}
                  aria-busy={downloading}
                  aria-label={t(($) => $.image.download)}
                >
                  {downloading ? (
                    <Loader2 className="size-3.5 animate-spin" />
                  ) : (
                    <Download className="size-3.5" />
                  )}
                </button>
              }
            />
            <DeferredTooltip
              content={t(($) => $.image.copy_link)}
              trigger={
                <button type="button" onClick={handleCopyLink} aria-label={t(($) => $.image.copy_link)}>
                  <LinkIcon className="size-3.5" />
                </button>
              }
            />
            {editable && onDelete && (
              <DeferredTooltip
                content={t(($) => $.image.delete)}
                trigger={
                  <button type="button" onClick={onDelete} aria-label={t(($) => $.image.delete)}>
                    <Trash2 className="size-3.5" />
                  </button>
                }
              />
            )}
          </span>
        )}
      </span>
    </span>
  );
}
