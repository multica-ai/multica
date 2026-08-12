"use client";

/**
 * AttachmentPreviewModal — full-screen inline preview for an attachment.
 *
 * Single modal for every previewable kind. Handles 7 PreviewKinds:
 *
 *   - image : <img> on the shared ZoomCanvas — fit on open, then wheel /
 *             drag / pinch / double-click / keyboard zoom, same controls as
 *             the Mermaid viewer. Replaces the previous standalone
 *             ImageLightbox.
 *   - pdf   : <iframe src={download_url}> — relies on Chromium's PDFium
 *             plugin. On desktop, requires webPreferences.plugins=true
 *             (see apps/desktop/src/main/index.ts).
 *   - video : <video controls src={download_url}>
 *   - audio : <audio controls src={download_url}>
 *
 *   - markdown : fetch text via api.getAttachmentTextContent, render via
 *                the existing ReadonlyContent (full mention/mermaid/katex
 *                pipeline included).
 *   - html     : fetch text, hand to <iframe srcdoc={text}
 *                sandbox="allow-scripts">. The iframe runs in an opaque
 *                origin: scripts execute (chart libraries / vanilla SVG
 *                JS work), but cookie / localStorage / parent access /
 *                top-navigation / popups / forms stay blocked because
 *                `allow-same-origin` is intentionally NOT included.
 *   - text     : fetch text, highlight with lowlight if the extension
 *                maps to a known hljs language; otherwise plain <pre>.
 *
 * Media types load directly from the CloudFront signed `download_url`.
 * Text types go through `/api/attachments/{id}/content` to sidestep
 * CloudFront CORS (not configured) + Content-Disposition: attachment.
 */

import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import {
  PreviewTooLargeError,
  PreviewUnsupportedError,
} from "@multica/core/api";
import {
  ChevronLeft,
  ChevronRight,
  Download,
  ExternalLink,
  FileText,
  Loader2,
  X,
} from "lucide-react";
import type { Attachment } from "@multica/core/types";
import { paths, useWorkspaceSlug } from "@multica/core/paths";
import { cn } from "@multica/ui/lib/utils";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import {
  UI_EASE_OUT,
  UI_MOTION_DURATION,
} from "@multica/ui/lib/motion";
import { useT } from "../i18n";
import { useNavigation } from "../navigation";
import { openExternal } from "../platform";
import { ReadonlyContent } from "./readonly-content";
import {
  extensionToLanguage,
  getPreviewKind,
  type PreviewKind,
} from "./utils/preview";
import { useDownloadAttachment } from "./use-download-attachment";
import { useAttachmentHtmlText } from "./hooks/use-attachment-html-text";
import { useResignedInlineMediaURL } from "./hooks/use-inline-media-url";
import { useZoomCanvas, type ZoomCanvasApi } from "./hooks/use-zoom-canvas";
import { ZoomCanvas, ZoomControls } from "./zoom-canvas";
import type { Size } from "./utils/zoom-transform";
import { HtmlPreviewBody } from "./html-preview-body";
import { CodeBlockStatic } from "./code-block-static";

// ---------------------------------------------------------------------------
// Preview source — full attachment, or URL-only (media types only)
// ---------------------------------------------------------------------------
//
// `full` carries the resolved Attachment record and supports every PreviewKind
// (text types require the attachment id to call /api/attachments/{id}/content).
//
// `url` carries just the signed URL + filename. It is what NodeViews fall back
// to when `resolveAttachment(href)` returns undefined — typical when the URL
// was copy-pasted across comments so the attachment record isn't reachable
// from the current entity's `attachments` prop. Only media kinds (pdf / video
// / audio) can be opened from a `url` source because those render directly
// from the URL without hitting the text-content proxy.

export type PreviewSource =
  | { kind: "full"; attachment: Attachment }
  | { kind: "url"; url: string; filename: string };

// PreviewKinds that can render from a URL-only source. Text-based kinds
// (markdown / html / text) need the /content proxy which is ID-keyed.
const URL_ONLY_KINDS = new Set<PreviewKind>(["image", "pdf", "video", "audio"]);

// Normalized view used everywhere downstream of `useAttachmentPreview`.
// `attachmentId === null` signals URL-only mode (download falls back to
// `openExternal`, text rendering branches are unreachable by the gate).
interface PreviewState {
  filename: string;
  contentType: string;
  mediaUrl: string;
  attachmentId: string | null;
}

function resolvePreviewMediaUrl(attachment: Attachment): string {
  const raw =
    attachment.download_url || attachment.markdown_url || attachment.url;
  return resolvePublicFileUrl(raw) ?? raw;
}

function normalize(source: PreviewSource): PreviewState {
  // Resolve any server-relative URL (e.g. `/api/attachments/{id}/download`
  // returned by the unified-endpoint metadata path when no CloudFront
  // signer is configured) against the configured API base. Web with the
  // default empty base keeps the relative path and resolves it against
  // the page origin — same behaviour as before this PR. Desktop renderer
  // (loaded from `app://` / file: / dev-server origin) needs the absolute
  // form so `<img src>` / `<iframe src>` / `<video src>` actually point at
  // the API server instead of the shell origin.
  if (source.kind === "full") {
    return {
      filename: source.attachment.filename,
      contentType: source.attachment.content_type,
      mediaUrl: resolvePreviewMediaUrl(source.attachment),
      attachmentId: source.attachment.id,
    };
  }
  return {
    filename: source.filename,
    contentType: "",
    mediaUrl: resolvePublicFileUrl(source.url) ?? source.url,
    attachmentId: null,
  };
}

// ---------------------------------------------------------------------------
// Public props
// ---------------------------------------------------------------------------

/**
 * Position of this preview inside a surface's image sequence (MUL-5752).
 *
 * `onPrev` / `onNext` are undefined AT the boundaries — the sequence does not
 * wrap, so first/last simply disable the corresponding control. Supplied only
 * by `ImageSequenceProvider`; a standalone preview leaves this unset and
 * renders exactly as before.
 */
export interface PreviewSequence {
  /** 0-based. Rendered as `index + 1` of `total`. */
  index: number;
  total: number;
  onPrev?: () => void;
  onNext?: () => void;
}

interface AttachmentPreviewModalProps {
  source: PreviewSource;
  open: boolean;
  onClose: () => void;
  sequence?: PreviewSequence;
  /** Fired when the image kind fails to load — lets a gallery skip the frame. */
  onImageError?: () => void;
}

// ---------------------------------------------------------------------------
// Hook — local state + ready-to-mount modal JSX
// ---------------------------------------------------------------------------
//
// Why no React context / provider: packages/views/ cannot mount a Context.Provider
// inside CoreProvider (in packages/core/), and threading a new provider through
// every app layout is more friction than it's worth for a feature with at most
// one open modal at a time. Instead each entry point gets its own local state
// and renders the returned `modal` node. Multiple entry points coexisting just
// means each carries its own (collapsed) state — they never collide because
// only one preview is open per user click.

export interface AttachmentPreviewHandle {
  /** Try to open a preview for the source. Returns false when the file type
   *  isn't previewable, OR when the source is URL-only but the kind requires
   *  a full attachment (text/markdown/html). Callers can fall back to a
   *  download flow. */
  tryOpen: (source: PreviewSource) => boolean;
  /** Force-open a preview, skipping the previewable() guard. Use for cases
   *  where the caller has already filtered. */
  open: (source: PreviewSource) => void;
  /** Modal node to render somewhere in the caller's tree. Resolves to `null`
   *  when no preview is active. Safe to render inside any container — the
   *  modal portals to document.body. */
  modal: ReactNode;
}

export function useAttachmentPreview(): AttachmentPreviewHandle {
  const [current, setCurrent] = useState<PreviewSource | null>(null);
  const [previewOpen, setPreviewOpen] = useState(false);

  const open = useCallback((source: PreviewSource) => {
    setCurrent(source);
    setPreviewOpen(true);
  }, []);
  const tryOpen = useCallback((source: PreviewSource) => {
    const state = normalize(source);
    const kind = getPreviewKind(state.contentType, state.filename);
    if (!kind) return false;
    // URL-only sources cannot drive text kinds — the /content proxy is ID-keyed.
    if (source.kind === "url" && !URL_ONLY_KINDS.has(kind)) return false;
    setCurrent(source);
    setPreviewOpen(true);
    return true;
  }, []);

  const modal = useMemo(
    () =>
      current ? (
        <AttachmentPreviewModal
          source={current}
          open={previewOpen}
          onClose={() => setPreviewOpen(false)}
          onExitComplete={() => setCurrent(null)}
        />
      ) : null,
    [current, previewOpen],
  );

  return useMemo(() => ({ open, tryOpen, modal }), [open, tryOpen, modal]);
}

// ---------------------------------------------------------------------------
// Image swap without a blank frame
// ---------------------------------------------------------------------------

// Returns the last image URL that finished decoding, holding the previous one
// on screen while the next downloads. Swapping `<img src>` (or remounting the
// panel) the moment navigation happens blanks the canvas for the full
// network+decode gap; decode-then-swap is the standard lightbox fix.
//
// On load failure the hook reports the error and keeps the last good frame —
// when the whole remaining sequence is broken the reader stays on the last
// image that worked (with the "unavailable" toast) instead of a broken glyph.
//
// Engines without `Image.decode()` (jsdom in tests) swap immediately: the old
// pre-MUL-5752 behaviour, traded back for correctness there.
function useSettledImageURL(
  targetUrl: string,
  enabled: boolean,
  onLoadError?: () => void,
): string {
  const [settled, setSettled] = useState(targetUrl);
  const onErrorRef = useRef(onLoadError);
  onErrorRef.current = onLoadError;

  useEffect(() => {
    if (!enabled) return;
    if (!targetUrl) {
      setSettled(targetUrl);
      return;
    }
    let cancelled = false;
    const probe = new window.Image();
    if (typeof probe.decode !== "function") {
      setSettled(targetUrl);
      return;
    }
    probe.src = targetUrl;
    probe.decode().then(
      () => {
        if (!cancelled) setSettled(targetUrl);
      },
      () => {
        // Rejection covers both load failure and undecodable bytes.
        if (!cancelled) onErrorRef.current?.();
      },
    );
    return () => {
      cancelled = true;
    };
  }, [targetUrl, enabled]);

  return enabled ? settled : targetUrl;
}

// Warms the browser cache for a sequence neighbour so paging to it swaps
// without a visible wait: runs the same URL re-sign the panel itself would,
// then fetches the bytes through a detached <img>. Renders nothing.
export function PreviewImagePrefetch({ source }: { source: PreviewSource }) {
  const state = normalize(source);
  const url = useResignedInlineMediaURL(
    state.attachmentId ?? undefined,
    state.mediaUrl,
    true,
  );

  useEffect(() => {
    if (!url) return;
    const probe = new window.Image();
    probe.src = url;
  }, [url]);

  return null;
}

// ---------------------------------------------------------------------------
// Modal — frame + dispatch
// ---------------------------------------------------------------------------

export function AttachmentPreviewModal({
  source,
  open,
  onClose,
  onExitComplete,
  sequence,
  onImageError,
}: AttachmentPreviewModalProps & { onExitComplete?: () => void }) {
  const download = useDownloadAttachment();
  const shouldReduceMotion = useReducedMotion() ?? false;
  const state = normalize(source);
  // useWorkspaceSlug (not useWorkspacePaths) — returns null outside a
  // workspace route instead of throwing, so the new-tab button just hides.
  const slug = useWorkspaceSlug();
  const navigation = useNavigation();

  const onPrev = sequence?.onPrev;
  const onNext = sequence?.onNext;

  useEffect(() => {
    if (!open) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        onClose();
        return;
      }
      // Arrow navigation only when this preview is part of a sequence. The
      // zoom canvas gives its horizontal arrows up in that case (see
      // `horizontalArrowPan` below), so exactly one of the two responds.
      // Modified presses stay with the browser / OS.
      if (e.metaKey || e.ctrlKey || e.altKey || e.shiftKey) return;
      if (e.key === "ArrowLeft" && onPrev) {
        e.preventDefault();
        onPrev();
      } else if (e.key === "ArrowRight" && onNext) {
        e.preventDefault();
        onNext();
      }
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [open, onClose, onPrev, onNext]);

  const kind = getPreviewKind(state.contentType, state.filename);

  // Download dispatcher: re-sign through `getAttachment` when an id is
  // available; otherwise fall back to opening the (possibly stale) URL
  // externally — same tradeoff as the file-card NodeView's download path.
  const handleDownload = () => {
    if (state.attachmentId) {
      download(state.attachmentId);
    } else {
      openExternal(state.mediaUrl);
    }
  };

  // Open-in-new-tab mirrors HtmlAttachmentPreview's inline toolbar: only the
  // `html` kind has a dedicated full-page route (/attachments/{id}/preview).
  // Gated on slug + attachmentId for the same reason — URL-only sources
  // can't address the /content proxy the page relies on.
  const canOpenInNewTab = kind === "html" && !!slug && !!state.attachmentId;
  const handleOpenInNewTab = () => {
    if (!slug || !state.attachmentId) return;
    const nameQuery = state.filename
      ? `?name=${encodeURIComponent(state.filename)}`
      : "";
    const path = `${paths.workspace(slug).attachmentPreview(state.attachmentId)}${nameQuery}`;
    if (navigation.openInNewTab) {
      navigation.openInNewTab(path, state.filename, { activate: true });
    } else {
      const url = navigation.getShareableUrl(path);
      window.open(url, "_blank", "noopener,noreferrer");
    }
    onClose();
  };

  if (typeof document === "undefined") return null;

  return createPortal(
    <AnimatePresence onExitComplete={onExitComplete}>
      {open && (
        <motion.div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-4"
          // Only a click that lands on the backdrop itself closes. A pan that
          // starts on the zoom canvas and releases out here retargets its
          // click through pointer capture, but this makes the intent explicit
          // instead of relying on that.
          onClick={(e) => {
            if (e.target === e.currentTarget) onClose();
          }}
          role="dialog"
          aria-modal="true"
          aria-label={state.filename}
          initial={{ opacity: 0 }}
          animate={{
            opacity: 1,
            transition: {
              duration: UI_MOTION_DURATION.fast,
              ease: UI_EASE_OUT,
            },
          }}
          exit={{
            opacity: 0,
            transition: {
              duration: UI_MOTION_DURATION.fast,
              ease: UI_EASE_OUT,
            },
          }}
        >
          {/* Larger than the create-issue dialog (max-w-4xl, manualDialogContentClass)
              because PDF / video previews want more room. Capped to viewport
              minus the surrounding p-4 (1rem each side) so it never overflows
              the screen on small displays / split panes. */}
          <motion.div
            className="flex h-[min(90vh,calc(100vh-2rem))] w-full max-w-6xl flex-col overflow-hidden rounded-lg bg-background shadow-xl"
            onClick={(e) => e.stopPropagation()}
            initial={{
              opacity: 0,
              transform: shouldReduceMotion ? "scale(1)" : "scale(0.95)",
            }}
            animate={{
              opacity: 1,
              transform: "scale(1)",
              transition: {
                duration: UI_MOTION_DURATION.standard,
                ease: UI_EASE_OUT,
              },
            }}
            exit={{
              opacity: 0,
              transform: shouldReduceMotion ? "scale(1)" : "scale(0.95)",
              transition: {
                duration: UI_MOTION_DURATION.fast,
                ease: UI_EASE_OUT,
              },
            }}
          >
            {/* Below the `open &&` gate on purpose: the panel's zoom state is
                destroyed on close, so every open re-fits instead of restoring
                a stale zoom from the last time this image was viewed.

                Deliberately NOT keyed on the file: remounting the panel on
                sequence navigation blanks the canvas for the whole
                network+decode gap. The panel persists and swaps the image
                only once the next one has decoded (`useSettledImageURL`).
                Zoom still resets per image — `natural` passes through null on
                every swap, so the canvas re-fits even across a run of
                same-resolution screenshots. */}
            <PreviewPanel
              kind={kind}
              source={source}
              state={state}
              onClose={onClose}
              onDownload={handleDownload}
              onOpenInNewTab={canOpenInNewTab ? handleOpenInNewTab : undefined}
              sequence={sequence}
              onImageError={onImageError}
            />
          </motion.div>
        </motion.div>
      )}
    </AnimatePresence>,
    document.body,
  );
}

// ---------------------------------------------------------------------------
// Panel — header + content area
// ---------------------------------------------------------------------------

// Header chrome and the content area live together because the image kind's
// zoom controls sit in the header while the canvas they drive is the body:
// one owner for that shared state, mounted and destroyed with the open modal.
function PreviewPanel({
  kind,
  source,
  state,
  onClose,
  onDownload,
  onOpenInNewTab,
  sequence,
  onImageError,
}: {
  kind: PreviewKind | null;
  source: PreviewSource;
  state: PreviewState;
  onClose: () => void;
  onDownload: () => void;
  onOpenInNewTab?: () => void;
  sequence?: PreviewSequence;
  onImageError?: () => void;
}) {
  const { t } = useT("editor");

  // Gallery navigation hands this panel an attachment the reader never
  // clicked, so — unlike the click-through path, where <Attachment> had
  // already upgraded the URL — the modal has to run the re-sign itself. A
  // no-op for URLs that are already loadable (signed CDN, public storage).
  const targetUrl = useResignedInlineMediaURL(
    state.attachmentId ?? undefined,
    state.mediaUrl,
    kind === "image",
  );
  // The previous image stays on the canvas until this one has decoded — the
  // swap itself is what used to flash. Also absorbs the re-sign URL upgrade
  // (raw -> signed) without a second visible load.
  const mediaUrl = useSettledImageURL(targetUrl, kind === "image", onImageError);

  // Natural size is carried with the URL it was measured from, so a panel
  // reused for a different attachment can never fit the new image against the
  // old one's dimensions.
  const [measured, setMeasured] = useState<{ url: string; size: Size } | null>(
    null,
  );
  const natural =
    kind === "image" && measured?.url === mediaUrl ? measured.size : null;
  // Left / right arrows belong to the sequence when there is one; the canvas
  // keeps them for panning otherwise. Vertical arrows always pan, and a
  // zoomed image still pans horizontally by drag / wheel.
  const canvas = useZoomCanvas({
    content: natural,
    horizontalArrowPan: !sequence,
  });

  const handleNaturalSize = useCallback(
    (url: string, size: Size) => {
      setMeasured((previous) =>
        previous?.url === url &&
        previous.size.width === size.width &&
        previous.size.height === size.height
          ? previous
          : { url, size },
      );
    },
    [],
  );

  return (
    <>
      <div className="flex items-center gap-2 border-b border-border bg-muted/30 px-4 py-2">
        <FileText className="size-4 shrink-0 text-muted-foreground" />
        {/* Baseline group: filename (text-body) and type (text-caption) are
            different type sizes on one line — the row's items-center would
            center their unequal line boxes and visibly offset the smaller
            text. Mixed-size text aligns by baseline. */}
        <div className="flex min-w-0 items-baseline gap-2">
          <p className="truncate text-body font-medium">{state.filename}</p>
          <span className="shrink-0 text-caption text-muted-foreground">
            {state.contentType || "—"}
          </span>
        </div>
        <div className="ml-auto flex items-center gap-1">
          {/* Navigation leads the action cluster, arrows off the image
              (they covered exactly the content being looked at) and the
              counter between the arrows it describes. min-w keeps the
              arrows from shifting as digit counts change. */}
          {sequence && (
            <div className="mr-1 flex shrink-0 items-center gap-0.5">
              <SequenceButton
                side="prev"
                label={t(($) => $.image.previous)}
                onClick={sequence.onPrev}
              />
              <span
                className="min-w-10 select-none text-center text-caption tabular-nums text-muted-foreground"
                aria-live="polite"
              >
                {t(($) => $.image.sequence_position, {
                  index: sequence.index + 1,
                  total: sequence.total,
                })}
              </span>
              <SequenceButton
                side="next"
                label={t(($) => $.image.next)}
                onClick={sequence.onNext}
              />
            </div>
          )}
          {/* Standalone preview keeps the original gate — no controls until
              the image is measured, and none at all for content that has no
              intrinsic size to drive. In a sequence they stay mounted
              (disabled while un-measured) instead: `natural` passes through
              null on every swap, and controls that vanish and reappear shift
              the buttons to their right on every navigation. */}
          {kind === "image" && (natural || sequence) && (
            <ZoomControls canvas={canvas} disabled={!natural} />
          )}
          {onOpenInNewTab && (
            <button
              type="button"
              className="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
              title={t(($) => $.attachment.open_in_new_tab)}
              aria-label={t(($) => $.attachment.open_in_new_tab)}
              onClick={onOpenInNewTab}
            >
              <ExternalLink className="size-4" />
            </button>
          )}
          <button
            type="button"
            className="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
            title={t(($) => $.image.download)}
            aria-label={t(($) => $.image.download)}
            onClick={onDownload}
          >
            <Download className="size-4" />
          </button>
          <button
            type="button"
            className="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
            title={t(($) => $.attachment.close)}
            aria-label={t(($) => $.attachment.close)}
            onClick={onClose}
          >
            <X className="size-4" />
          </button>
        </div>
      </div>
      {/* Image gets a flex column: the canvas sizes itself with `flex: 1 1
          auto` and its content is absolutely positioned, so in a plain block
          parent it would collapse to zero height and show nothing. It also
          clips and handles its own wheel events — letting this wrapper scroll
          too would fight the pan. Every other kind keeps the block scroller;
          making them flex items would let tall text previews shrink to fit
          instead of scrolling. */}
      <div
        className={cn(
          "relative min-h-0 flex-1 bg-background",
          kind === "image" ? "flex flex-col overflow-hidden" : "overflow-auto",
        )}
      >
        {kind === "image" ? (
          <ImagePreview
            state={state}
            mediaUrl={mediaUrl}
            canvas={canvas}
            natural={natural}
            onNaturalSize={handleNaturalSize}
            onError={onImageError}
          />
        ) : (
          <PreviewContent
            kind={kind}
            source={source}
            state={state}
            onDownload={onDownload}
          />
        )}
      </div>
    </>
  );
}

// ---------------------------------------------------------------------------
// Sequence controls
// ---------------------------------------------------------------------------

// Header chevrons in the same idiom as the download/close buttons. `onClick`
// undefined means "boundary reached": the button stays mounted but disabled,
// so the reader can see they are at one end instead of the control vanishing
// and shifting the counter into its place. `enabled:hover` so the disabled
// state gets no hover feedback (and no pointer-events-none — a disabled
// control should still catch the cursor and read as "nothing here").
function SequenceButton({
  side,
  label,
  onClick,
}: {
  side: "prev" | "next";
  label: string;
  onClick?: () => void;
}) {
  const Icon = side === "prev" ? ChevronLeft : ChevronRight;
  return (
    <button
      type="button"
      className="rounded-md p-1.5 text-muted-foreground transition-colors enabled:hover:bg-secondary enabled:hover:text-foreground disabled:opacity-30"
      title={label}
      aria-label={label}
      disabled={!onClick}
      onClick={onClick}
    >
      <Icon className="size-4" />
    </button>
  );
}

// ---------------------------------------------------------------------------
// Image — zoom canvas
// ---------------------------------------------------------------------------

function ImagePreview({
  state,
  mediaUrl,
  canvas,
  natural,
  onNaturalSize,
  onError,
}: {
  state: PreviewState;
  mediaUrl: string;
  canvas: ZoomCanvasApi;
  natural: Size | null;
  onNaturalSize: (url: string, size: Size) => void;
  onError?: () => void;
}) {
  const { t } = useT("editor");
  const url = mediaUrl;

  const readNaturalSize = useCallback(
    (image: HTMLImageElement | null) => {
      // naturalWidth is 0 for an image that hasn't decoded yet, and also for
      // an SVG that declares only a viewBox — Chromium gives those no
      // intrinsic size at all. Both fall back to the letterboxed branch;
      // the first recovers on load, the second stays there.
      if (!image || image.naturalWidth <= 0 || image.naturalHeight <= 0) return;
      onNaturalSize(url, {
        width: image.naturalWidth,
        height: image.naturalHeight,
      });
    },
    [onNaturalSize, url],
  );

  return (
    <ZoomCanvas
      canvas={canvas}
      content={natural}
      label={t(($) => $.image.canvas_label)}
      className="bg-black/40"
      autoFocus
    >
      <img
        // A cached image is already `complete` before React attaches onLoad,
        // so that event never fires — measure from the ref as well.
        ref={readNaturalSize}
        onLoad={(e) => readNaturalSize(e.currentTarget)}
        onError={onError}
        src={url}
        alt={state.filename}
        className={cn(
          "select-none",
          natural
            ? "block size-full"
            : "max-h-full max-w-full rounded-lg object-contain",
        )}
        // Native image dragging would hijack the pan gesture.
        draggable={false}
      />
    </ZoomCanvas>
  );
}

// ---------------------------------------------------------------------------
// Dispatch
// ---------------------------------------------------------------------------

// Dispatch on PreviewKind. New cases go here; remember that the modal frame
// (header, close, Download CTA, ESC handling) is shared — sub-renderers only
// own the content area. `image` is handled by PreviewPanel itself because its
// toolbar and canvas share zoom state.
function PreviewContent({
  kind,
  source,
  state,
  onDownload,
}: {
  kind: Exclude<PreviewKind, "image"> | null;
  source: PreviewSource;
  state: PreviewState;
  onDownload: () => void;
}) {
  const { t } = useT("editor");

  if (kind === null) {
    return (
      <UnsupportedFallback
        message={t(($) => $.attachment.preview_unsupported)}
        onDownload={onDownload}
      />
    );
  }

  // Text kinds need the attachment id for the /content proxy. The tryOpen
  // gate prevents URL-only sources from reaching here for text kinds, but
  // be defensive — a direct mount of <AttachmentPreviewModal> with a URL
  // source whose filename later resolves to a text kind would otherwise
  // crash on a null id.
  if (
    (kind === "markdown" || kind === "html" || kind === "text") &&
    !state.attachmentId
  ) {
    return (
      <UnsupportedFallback
        message={t(($) => $.attachment.preview_unsupported)}
        onDownload={onDownload}
      />
    );
  }

  switch (kind) {
    case "pdf":
      return (
        <iframe
          src={state.mediaUrl}
          className="h-full w-full bg-background"
          title={state.filename}
        />
      );
    case "video":
      return (
        <div className="flex h-full w-full items-center justify-center bg-black">
          <video
            src={state.mediaUrl}
            controls
            className="h-full w-full object-contain"
          />
        </div>
      );
    case "audio":
      return (
        <div className="flex h-full w-full items-center justify-center p-8">
          <audio src={state.mediaUrl} controls className="w-full max-w-xl" />
        </div>
      );
    case "markdown":
      return (
        <TextBackedPreview
          attachmentId={state.attachmentId!}
          onDownload={onDownload}
          render={(text) => (
            <ReadonlyContent
              content={text}
              className="px-6 py-4"
              attachments={source.kind === "full" ? [source.attachment] : []}
            />
          )}
        />
      );
    case "html":
      return (
        <TextBackedPreview
          attachmentId={state.attachmentId!}
          onDownload={onDownload}
          render={(text) => (
            <HtmlPreviewBody
              source={{ kind: "inline", html: text }}
              title={state.filename}
              className="h-full w-full"
              iframeClassName="rounded-none border-0"
            />
          )}
        />
      );
    case "text":
      return (
        <TextBackedPreview
          attachmentId={state.attachmentId!}
          onDownload={onDownload}
          render={(text) => (
            <CodeBlockStatic
              language={extensionToLanguage(state.filename)}
              body={text}
              className="px-6 py-4"
            />
          )}
        />
      );
  }
}

// ---------------------------------------------------------------------------
// Text-backed preview — fetches body once, then hands to the render prop
// ---------------------------------------------------------------------------

// React Query owns server state per the project convention; re-opening the
// same attachment hits the cache instead of re-fetching. Query is keyed on
// the attachment id alone — the 30 min TTL on the server-side signed URL
// is much longer than any plausible preview session.
function TextBackedPreview({
  attachmentId,
  onDownload,
  render,
}: {
  attachmentId: string;
  onDownload: () => void;
  render: (text: string) => ReactNode;
}) {
  const { t } = useT("editor");
  const query = useAttachmentHtmlText(attachmentId);

  if (query.isLoading) {
    return (
      <div className="flex h-full items-center justify-center gap-2 text-body text-muted-foreground">
        <Loader2 className="size-4 animate-spin" />
        {t(($) => $.attachment.preview_loading)}
      </div>
    );
  }
  if (query.error) {
    if (query.error instanceof PreviewTooLargeError) {
      return (
        <UnsupportedFallback
          message={t(($) => $.attachment.preview_too_large)}
          onDownload={onDownload}
        />
      );
    }
    if (query.error instanceof PreviewUnsupportedError) {
      return (
        <UnsupportedFallback
          message={t(($) => $.attachment.preview_unsupported)}
          onDownload={onDownload}
        />
      );
    }
    return (
      <UnsupportedFallback
        message={t(($) => $.attachment.preview_failed)}
        onDownload={onDownload}
      />
    );
  }
  if (!query.data) return null;
  return <>{render(query.data.text)}</>;
}

// ---------------------------------------------------------------------------
// Fallback — used for 413 / 415 / unknown kinds
// ---------------------------------------------------------------------------

function UnsupportedFallback({
  message,
  onDownload,
}: {
  message: string;
  onDownload: () => void;
}) {
  const { t } = useT("editor");
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 px-8 text-center">
      <FileText className="size-8 text-muted-foreground" />
      <p className="text-body text-muted-foreground">{message}</p>
      <button
        type="button"
        className="inline-flex items-center gap-2 rounded-md border border-border bg-background px-3 py-1.5 text-body transition-colors hover:bg-muted"
        onClick={onDownload}
      >
        <Download className="size-4" />
        {t(($) => $.image.download)}
      </button>
    </div>
  );
}

// Re-export the predicate from the dispatch util so entry-point components
// only need a single import to gate the Eye button.
export { isPreviewable } from "./utils/preview";
