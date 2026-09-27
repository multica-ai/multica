"use client";

/**
 * AttachmentPreviewModal — full-window viewer for an attachment.
 *
 * The file gets the whole window (MUL-7642): no card, no max width. A fixed
 * near-black stage sits behind the content whatever the theme — a photo, a
 * PDF page and a white HTML document all read against it — and the chrome
 * over it (top bar, prev / next, messages on the stage) wears the dark token
 * set via a `dark` class on each chrome element. Documents that are read
 * rather than looked at (Markdown, text) render on a centered sheet that
 * keeps the app's own theme.
 *
 * Single viewer for every previewable kind. Handles 9 PreviewKinds:
 *
 *   - image : <img> on the shared ZoomCanvas — fit on open, then wheel /
 *             drag / pinch / double-click / keyboard zoom, same controls as
 *             the Mermaid viewer.
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
 *                Laid out at a device width on demand, or shown as source.
 *   - table    : fetch text, parse CSV / TSV, sortable virtualized table.
 *   - structured : fetch text, parse JSON / JSON Lines / YAML into a
 *                collapsible tree, with the source one toggle away.
 *   - text     : fetch text, highlight with lowlight if the extension
 *                maps to a known hljs language, with numbered lines and a
 *                wrap toggle.
 *
 * The same top bar and stage also stand alone as the "open in new tab" page
 * (`AttachmentPreviewStandalone`), so a file looks the same in both.
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
  type CSSProperties,
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
  Code,
  Download,
  ExternalLink,
  File,
  FileAudio,
  FileCode,
  FileSpreadsheet,
  FileText,
  FileVideo,
  ImageIcon,
  LayoutGrid,
  ListTree,
  Loader2,
  Maximize2,
  MessageSquareText,
  Monitor,
  PanelRight,
  Smartphone,
  Tablet,
  WrapText,
  X,
  type LucideIcon,
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
import { useImmersiveMode } from "../platform/use-immersive-mode";
import { ReadonlyContent } from "./readonly-content";
import {
  canOpenPreview,
  extensionToLanguage,
  fileTypeLabel,
  getPreviewKind,
  structuredFormat,
  tableDelimiter,
  wrapsByDefault,
  type PreviewKind,
} from "./utils/preview";
import { parseStructured } from "./utils/parse-structured";
import { formatBytes } from "../common/format-bytes";
import { useDownloadAttachment } from "./use-download-attachment";
import { useAttachmentHtmlText } from "./hooks/use-attachment-html-text";
import { useResignedInlineMedia } from "./hooks/use-inline-media-url";
import { useZoomCanvas, type ZoomCanvasApi } from "./hooks/use-zoom-canvas";
import { ZoomCanvas, ZoomControls } from "./zoom-canvas";
import type { Size } from "./utils/zoom-transform";
import { HtmlPreviewBody } from "./html-preview-body";
import { CodeBlockStatic } from "./code-block-static";
import { TablePreview } from "./table-preview";
import { StructuredTree } from "./structured-tree";
import {
  HTML_VIEWPORTS,
  HtmlViewportFrame,
  type HtmlViewport,
} from "./html-viewport-frame";

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
  | {
      kind: "url";
      url: string;
      filename: string;
      /**
       * What the call site already knows this source to be. A URL-only source
       * has no content-type, so without it the modal can only re-derive the
       * kind from `filename` — and for a body image that "filename" is the
       * markdown caption, which is prose, not a file name. `![报告图表](…png)`
       * then reads as an extension-less unknown and the reader is told the
       * image can't be previewed (MUL-7518). Callers that know the slot is
       * definitionally an image (markdown `![]()`, the Tiptap image node, any
       * member of an image sequence) pass it through instead of guessing.
       */
      forceKind?: PreviewKind;
    };

// Normalized view used everywhere downstream of `useAttachmentPreview`.
// `attachmentId === null` signals URL-only mode (download falls back to
// `openExternal`, text rendering branches are unreachable by the gate).
interface PreviewState {
  filename: string;
  contentType: string;
  mediaUrl: string;
  attachmentId: string | null;
  /** 0 when unknown (URL-only source). */
  sizeBytes: number;
  /**
   * The kind every consumer dispatches on — resolved once, here, so the
   * tryOpen gate and the rendered panel can never disagree about what the
   * source is. A URL-only source's `forceKind` wins over autodetect; a full
   * attachment always has server metadata to detect from.
   */
  kind: PreviewKind | null;
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
      sizeBytes: source.attachment.size_bytes,
      kind: getPreviewKind(
        source.attachment.content_type,
        source.attachment.filename,
      ),
    };
  }
  return {
    filename: source.filename,
    contentType: "",
    mediaUrl: resolvePublicFileUrl(source.url) ?? source.url,
    attachmentId: null,
    sizeBytes: 0,
    kind: source.forceKind ?? getPreviewKind("", source.filename),
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

/** "Show where this was posted" — labelled by the host, which knows where. */
export interface PreviewLocateAction {
  label: string;
  onSelect: () => void;
}

/** The info panel beside the stage (MUL-7649). Its open state outlives the file. */
export interface PreviewInfoPanel {
  content: ReactNode;
  open: boolean;
  onToggle: () => void;
}

interface AttachmentPreviewModalProps {
  source: PreviewSource;
  open: boolean;
  onClose: () => void;
  sequence?: PreviewSequence;
  /** Fired when the image kind fails to load — lets a gallery skip the frame. */
  onImageError?: () => void;
  /** Rendered after the file name, e.g. a version switcher. */
  titleAccessory?: ReactNode;
  /** Adds the info button and `I`. */
  info?: PreviewInfoPanel;
  /** Adds "show where this was posted": the caller closes the viewer and scrolls. */
  locate?: PreviewLocateAction;
  /** Adds the overview button and `G`. */
  onOpenOverview?: () => void;
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
    const { kind } = normalize(source);
    // URL-only sources cannot drive text kinds — the /content proxy is ID-keyed.
    if (!canOpenPreview(kind, source.kind === "full")) return false;
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
// Only a URL that actually decoded as an image is ever held. The panel is
// reused across kinds, so arriving at an image from a PDF or a document has no
// previous frame: the image shows as itself and loads in place — handing the
// PDF's URL to <img> would fail and be blamed on the image being opened.
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
  const [settled, setSettled] = useState<string | null>(null);
  const onErrorRef = useRef(onLoadError);
  onErrorRef.current = onLoadError;

  useEffect(() => {
    if (!enabled) {
      setSettled(null);
      return;
    }
    // Nothing loadable yet (the URL is still being re-signed): keep holding
    // whatever frame is up.
    if (!targetUrl) return;
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

  return enabled && settled !== null ? settled : targetUrl;
}

// Warms the browser cache for a sequence neighbour so paging to it swaps
// without a visible wait: runs the same URL re-sign the panel itself would,
// then fetches the bytes through a detached <img>. Renders nothing.
export function PreviewImagePrefetch({ source }: { source: PreviewSource }) {
  const state = normalize(source);
  const { url, pending } = useResignedInlineMedia(
    state.attachmentId ?? undefined,
    state.mediaUrl,
    true,
  );

  useEffect(() => {
    if (!url || pending) return;
    const probe = new window.Image();
    probe.src = url;
  }, [url, pending]);

  return null;
}

// ---------------------------------------------------------------------------
// Viewer — frame + dispatch
// ---------------------------------------------------------------------------

// Desktop window chrome. The viewer covers the whole window, including the
// top bar's drag region, so it declares its own: the viewer is `no-drag`
// (a drag region underneath would otherwise swallow clicks on its controls),
// its top bar drags the window, and the controls in that bar opt back out.
// Chromium-only CSS; browsers ignore it.
const NO_DRAG = { WebkitAppRegion: "no-drag" } as CSSProperties;
const DRAG = { WebkitAppRegion: "drag" } as CSSProperties;

// A focused player or field owns its arrow keys (seek, caret) — the sequence
// only takes them when nothing else would.
function ownsArrowKeys(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable) return true;
  return target.closest("input, textarea, select, video, audio") !== null;
}

// Letter shortcuts stand down only where the reader could be typing.
function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable) return true;
  return target.closest("input, textarea, select") !== null;
}

export function AttachmentPreviewModal({
  source,
  open,
  onClose,
  onExitComplete,
  sequence,
  onImageError,
  titleAccessory,
  info,
  locate,
  onOpenOverview,
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
  const onToggleInfo = info?.onToggle;

  // macOS desktop: hide the traffic lights while the viewer is up — its top
  // bar starts at the window's top-left corner, where they would sit on the
  // file name. No-op on web and other platforms.
  useImmersiveMode(open);

  useEffect(() => {
    if (!open) return;
    const handler = (e: KeyboardEvent) => {
      // A menu opened from the chrome (the version switcher) owns its keys:
      // Escape closes the menu, not the viewer, and arrows move its focus.
      if (e.target instanceof Element && e.target.closest('[role="menu"]')) return;
      if (e.key === "Escape") {
        onClose();
        return;
      }
      // Arrow navigation only when this preview is part of a sequence. The
      // zoom canvas gives its horizontal arrows up in that case (see
      // `horizontalArrowPan` below), so exactly one of the two responds.
      // Modified presses stay with the browser / OS.
      if (e.metaKey || e.ctrlKey || e.altKey || e.shiftKey) return;
      if (!e.repeat && !isTypingTarget(e.target)) {
        // Claimed with preventDefault so the app's own single-key shortcuts
        // (which skip handled events) don't also fire behind the viewer.
        const letter = e.key.toLowerCase();
        if (letter === "g" && onOpenOverview) {
          e.preventDefault();
          onOpenOverview();
          return;
        }
        if (letter === "i" && onToggleInfo) {
          e.preventDefault();
          onToggleInfo();
          return;
        }
      }
      if (ownsArrowKeys(e.target)) return;
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
  }, [open, onClose, onPrev, onNext, onOpenOverview, onToggleInfo]);

  const kind = state.kind;

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

  // Open-in-new-tab: the full-page route (/attachments/{id}/preview) shows
  // every previewable kind, but loads the file by id — a URL-only source has
  // none, and a file the viewer cannot show gains nothing from a tab.
  const canOpenInNewTab = kind !== null && !!slug && !!state.attachmentId;
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
          // Blurred as well as dimmed: at any opacity that still reads as a
          // backdrop, the page's text shows through behind the top bar.
          className="fixed inset-0 z-50 flex flex-col bg-black/95 backdrop-blur-xl"
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
          style={NO_DRAG}
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
            surface="viewer"
            kind={kind}
            source={source}
            state={state}
            onClose={onClose}
            onDownload={handleDownload}
            onOpenInNewTab={canOpenInNewTab ? handleOpenInNewTab : undefined}
            sequence={sequence}
            onImageError={onImageError}
            reduceMotion={shouldReduceMotion}
            titleAccessory={titleAccessory}
            info={info}
            locate={locate}
            onOpenOverview={onOpenOverview}
          />
        </motion.div>
      )}
    </AnimatePresence>,
    document.body,
  );
}

// ---------------------------------------------------------------------------
// Panel — top bar + stage
// ---------------------------------------------------------------------------

const KIND_ICONS: Record<PreviewKind, LucideIcon> = {
  image: ImageIcon,
  pdf: FileText,
  video: FileVideo,
  audio: FileAudio,
  markdown: FileText,
  html: FileCode,
  table: FileSpreadsheet,
  structured: FileCode,
  text: FileCode,
};

type StructuredView = "tree" | "raw";

const VIEWPORT_ICONS: Record<HtmlViewport, LucideIcon> = {
  fill: Maximize2,
  desktop: Monitor,
  tablet: Tablet,
  phone: Smartphone,
};

/**
 * Renders an HTML attachment's iframe. The viewer's is plain; the full-page
 * preview's also reports its scroll position to the desktop tab restorer.
 */
export type HtmlFrameRenderer = (props: { html: string; title: string }) => ReactNode;

const renderViewerHtmlFrame: HtmlFrameRenderer = ({ html, title }) => (
  <HtmlPreviewBody
    html={html}
    title={title}
    className="h-full w-full"
    iframeClassName="rounded-none border-0"
  />
);

// The parsed body of a `structured` file. Parsed here, above the stage,
// because the top bar needs the outcome too: a file that does not parse has
// no tree to switch to. Shares the stage's query, so nothing is fetched twice.
function useStructuredParse(state: PreviewState, enabled: boolean) {
  const format = enabled ? structuredFormat(state.contentType, state.filename) : null;
  const query = useAttachmentHtmlText(format ? state.attachmentId : null);
  const text = query.data?.text;
  return useMemo(
    () => (format && text !== undefined ? parseStructured(text, format) : null),
    [format, text],
  );
}

// Top bar and stage live together because some kinds' controls sit in the
// bar while what they drive is on the stage — the image's zoom, a document's
// line wrapping, an HTML file's viewport: one owner for that shared state,
// mounted and destroyed with the open viewer.
//
// `surface` is where it is mounted: the viewer overlay, or the "open in new
// tab" page, which has no close, no sequence and no window drag region.
function PreviewPanel({
  surface,
  kind,
  source,
  state,
  onClose,
  onDownload,
  onOpenInNewTab,
  sequence,
  onImageError,
  reduceMotion,
  titleAccessory,
  info,
  locate,
  onOpenOverview,
  renderHtmlFrame = renderViewerHtmlFrame,
}: {
  surface: "viewer" | "page";
  kind: PreviewKind | null;
  source: PreviewSource;
  state: PreviewState;
  onClose?: () => void;
  onDownload: () => void;
  onOpenInNewTab?: () => void;
  sequence?: PreviewSequence;
  onImageError?: () => void;
  reduceMotion: boolean;
  titleAccessory?: ReactNode;
  info?: PreviewInfoPanel;
  locate?: PreviewLocateAction;
  onOpenOverview?: () => void;
  renderHtmlFrame?: HtmlFrameRenderer;
}) {
  const { t } = useT("editor");

  // How the file is shown. Held here rather than in the stage so a choice
  // carries over as the reader pages through the sequence: the next HTML
  // file opens at the same width, the next log wraps the same way. Wrapping
  // follows the file until the reader picks.
  const [wrapChoice, setWrapChoice] = useState<boolean | null>(null);
  const wrap = wrapChoice ?? wrapsByDefault(state.filename);
  const toggleWrap = () => setWrapChoice(!wrap);
  const [htmlViewport, setHtmlViewport] = useState<HtmlViewport>("fill");
  const [htmlSource, setHtmlSource] = useState(false);
  const [structuredChoice, setStructuredChoice] = useState<StructuredView>("tree");
  const structuredParse = useStructuredParse(state, kind === "structured");
  const treeAvailable = structuredParse?.ok !== false;
  const structuredView: StructuredView = treeAvailable ? structuredChoice : "raw";
  const view: StageView = {
    wrap,
    htmlViewport,
    htmlSource,
    structuredView,
    structuredParse,
    renderHtmlFrame,
  };

  // Gallery navigation hands this panel an attachment the reader never
  // clicked, so — unlike the click-through path, where <Attachment> had
  // already upgraded the URL — the viewer has to run the re-sign itself. A
  // no-op for URLs that are already loadable (signed CDN, public storage).
  const resigned = useResignedInlineMedia(
    state.attachmentId ?? undefined,
    state.mediaUrl,
    kind === "image",
  );
  // Until that upgrade lands the picked URL may be one this client cannot
  // load natively (desktop, split-origin web); handed to <img> it would fail
  // and, in a sequence, read as a broken image to skip. Hold the previous
  // frame — or show loading — instead.
  const targetUrl = resigned.pending ? "" : resigned.url;
  // The previous image stays on the canvas until this one has decoded — the
  // swap itself is what used to flash. Also absorbs the re-sign URL upgrade
  // (raw -> signed) without a second visible load.
  const mediaUrl = useSettledImageURL(targetUrl, kind === "image", onImageError);
  // A load error from the <img> belongs to the file being opened only when
  // that is what it shows — a frame held from the previous image never
  // reports against the next one.
  const imageLoadError =
    mediaUrl !== "" && mediaUrl === targetUrl ? onImageError : undefined;

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

  // What the reader wants to know about the file at a glance — type, pixel
  // size, weight. Not the MIME type: `image/png` says nothing `PNG` doesn't.
  const meta = [
    fileTypeLabel(state.filename),
    natural ? `${natural.width} × ${natural.height}` : "",
    state.sizeBytes > 0 ? formatBytes(state.sizeBytes) : "",
  ].filter(Boolean);
  const KindIcon = kind ? KIND_ICONS[kind] : File;

  // The stage's own padding — the gutters around the content — counts as
  // backdrop: clicking there closes, clicking the content never does.
  const closeOnBackdrop = (e: React.MouseEvent) => {
    if (e.target === e.currentTarget) onClose?.();
  };

  const wrapButton = (enabled: boolean) => (
    <ChromeButton
      label={t(($) => $.attachment.wrap_lines)}
      pressed={enabled && wrap}
      disabled={!enabled}
      onClick={toggleWrap}
    >
      <WrapText className="size-4" />
    </ChromeButton>
  );
  const viewportLabels: Record<HtmlViewport, string> = {
    fill: t(($) => $.attachment.viewport_fill),
    desktop: t(($) => $.attachment.viewport_desktop),
    tablet: t(($) => $.attachment.viewport_tablet),
    phone: t(($) => $.attachment.viewport_phone),
  };

  return (
    <>
      {/* Three columns so the counter stays centered on the window while a
          long filename truncates before reaching it. */}
      <header
        className="dark @container grid h-14 shrink-0 grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center gap-4 pl-3 pr-2 text-foreground"
        style={surface === "viewer" ? DRAG : undefined}
      >
        <div className="flex min-w-0 items-center gap-2.5">
          <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-secondary text-muted-foreground">
            <KindIcon className="size-4" />
          </span>
          <div className="min-w-0">
            <div className="flex min-w-0 items-center gap-2" style={titleAccessory ? NO_DRAG : undefined}>
              <p className="truncate text-body font-medium">{state.filename}</p>
              {titleAccessory}
            </div>
            {meta.length > 0 && (
              <p className="truncate text-caption text-muted-foreground tabular-nums">
                {meta.join(" · ")}
              </p>
            )}
          </div>
        </div>
        <span
          className="select-none text-label tabular-nums text-muted-foreground"
          aria-live="polite"
        >
          {sequence
            ? t(($) => $.attachment.sequence_position, {
                index: sequence.index + 1,
                total: sequence.total,
              })
            : null}
        </span>
        <div
          className="flex items-center justify-self-end gap-0.5"
          style={NO_DRAG}
        >
          {/* Standalone preview keeps the original gate — no controls until
              the image is measured, and none at all for content that has no
              intrinsic size to drive. In a sequence they stay mounted
              (disabled while un-measured) instead: `natural` passes through
              null on every swap, and controls that vanish and reappear shift
              the buttons to their right on every navigation. */}
          {kind === "image" && (natural || sequence) && (
            <>
              <ZoomControls canvas={canvas} disabled={!natural} />
              <ChromeDivider />
            </>
          )}
          {/* View controls stay mounted, disabled where they do not apply —
              toggling the source view must not shift the button just pressed. */}
          {kind === "html" && (
            <>
              <ChromeSegmented
                label={t(($) => $.attachment.viewport)}
                value={htmlViewport}
                disabled={htmlSource}
                options={HTML_VIEWPORTS.map((viewport) => ({
                  value: viewport,
                  label: viewportLabels[viewport],
                  icon: VIEWPORT_ICONS[viewport],
                }))}
                onChange={setHtmlViewport}
              />
              <ChromeButton
                label={t(($) => $.attachment.view_source)}
                pressed={htmlSource}
                onClick={() => setHtmlSource((shown) => !shown)}
              >
                <Code className="size-4" />
              </ChromeButton>
              {wrapButton(htmlSource)}
              <ChromeDivider />
            </>
          )}
          {kind === "structured" && (
            <>
              <ChromeSegmented
                label={t(($) => $.attachment.view_mode)}
                value={structuredView}
                disabled={!treeAvailable}
                options={[
                  {
                    value: "tree",
                    label: t(($) => $.attachment.view_tree),
                    icon: ListTree,
                  },
                  {
                    value: "raw",
                    label: t(($) => $.attachment.view_raw),
                    icon: Code,
                  },
                ]}
                onChange={setStructuredChoice}
              />
              {wrapButton(structuredView === "raw")}
              <ChromeDivider />
            </>
          )}
          {kind === "text" && (
            <>
              {wrapButton(true)}
              <ChromeDivider />
            </>
          )}
          {locate && (
            <ChromeButton label={locate.label} onClick={locate.onSelect}>
              <MessageSquareText className="size-4" />
            </ChromeButton>
          )}
          {onOpenInNewTab && (
            <ChromeButton
              label={t(($) => $.attachment.open_in_new_tab)}
              onClick={onOpenInNewTab}
            >
              <ExternalLink className="size-4" />
            </ChromeButton>
          )}
          <ChromeButton label={t(($) => $.image.download)} onClick={onDownload}>
            <Download className="size-4" />
          </ChromeButton>
          {(onOpenOverview || info) && <ChromeDivider />}
          {onOpenOverview && (
            <ChromeButton
              label={t(($) => $.attachment.overview)}
              shortcut="G"
              onClick={onOpenOverview}
            >
              <LayoutGrid className="size-4" />
            </ChromeButton>
          )}
          {info && (
            <ChromeButton
              label={t(($) => $.attachment.info)}
              shortcut="I"
              pressed={info.open}
              onClick={info.onToggle}
            >
              <PanelRight className="size-4" />
            </ChromeButton>
          )}
          {onClose && (
            <>
              <ChromeDivider />
              <ChromeButton label={t(($) => $.attachment.close)} onClick={onClose}>
                <X className="size-4" />
              </ChromeButton>
            </>
          )}
        </div>
      </header>
      <div className="flex min-h-0 flex-1">
        {/* The stage. In a sequence its sides are 64px gutters holding the
            prev / next buttons, so a fitted image or a page never runs under
            them. */}
        <motion.div
          className={cn(
            "relative min-h-0 min-w-0 flex-1",
            sequence ? "px-16" : "px-4",
          )}
          onClick={closeOnBackdrop}
          initial={{ transform: reduceMotion ? "scale(1)" : "scale(0.97)" }}
          animate={{
            transform: "scale(1)",
            transition: {
              duration: UI_MOTION_DURATION.standard,
              ease: UI_EASE_OUT,
            },
          }}
        >
          {kind === "image" ? (
            <ImagePreview
              state={state}
              mediaUrl={mediaUrl}
              canvas={canvas}
              natural={natural}
              onNaturalSize={handleNaturalSize}
              onError={imageLoadError}
            />
          ) : (
            <PreviewContent
              kind={kind}
              source={source}
              state={state}
              view={view}
              onDownload={onDownload}
              onBackdropClick={closeOnBackdrop}
            />
          )}
          {sequence && (
            <>
              <SequenceButton
                side="prev"
                label={t(($) => $.attachment.previous)}
                onClick={sequence.onPrev}
              />
              <SequenceButton
                side="next"
                label={t(($) => $.attachment.next)}
                onClick={sequence.onNext}
              />
            </>
          )}
        </motion.div>
        {info?.open && (
          // A column beside the stage rather than a layer over it: the file
          // re-fits into what is left instead of hiding under the panel.
          <aside
            className="dark w-80 max-w-[45vw] shrink-0 overflow-y-auto border-l border-border bg-background/40 text-foreground"
            aria-label={t(($) => $.attachment.info)}
          >
            {info.content}
          </aside>
        )}
      </div>
    </>
  );
}

// ---------------------------------------------------------------------------
// Chrome controls
// ---------------------------------------------------------------------------

// Top-bar icon button. Sits inside the `dark` header, so the semantic tokens
// resolve to the dark set whatever the app theme is.
function ChromeButton({
  label,
  shortcut,
  pressed,
  disabled,
  onClick,
  children,
}: {
  label: string;
  /** Single-key shortcut, shown in the tooltip and announced to AT. */
  shortcut?: string;
  /** Toggle buttons pass their state; plain actions leave it unset. */
  pressed?: boolean;
  disabled?: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      className={cn(
        "flex size-8 items-center justify-center rounded-md transition-colors enabled:hover:bg-secondary enabled:hover:text-foreground disabled:opacity-40",
        pressed ? "bg-secondary text-foreground" : "text-muted-foreground",
      )}
      title={shortcut ? `${label} (${shortcut})` : label}
      aria-label={label}
      aria-keyshortcuts={shortcut}
      aria-pressed={pressed}
      disabled={disabled}
      onClick={onClick}
    >
      {children}
    </button>
  );
}

// A one-of-several choice in the top bar (viewport width, tree / raw). Same
// pressed look as the toggle buttons beside it. Labels show once the bar has
// room for them; below that the icons carry the choice, named by tooltip.
function ChromeSegmented<T extends string>({
  label,
  value,
  options,
  disabled,
  onChange,
}: {
  label: string;
  value: T;
  options: ReadonlyArray<{ value: T; label: string; icon: LucideIcon }>;
  disabled?: boolean;
  onChange: (value: T) => void;
}) {
  return (
    <div role="group" aria-label={label} className="flex items-center gap-0.5">
      {options.map((option) => {
        const pressed = option.value === value;
        const Icon = option.icon;
        return (
          <button
            key={option.value}
            type="button"
            className={cn(
              "flex h-8 min-w-8 items-center justify-center gap-1.5 rounded-md px-2 text-label transition-colors enabled:hover:bg-secondary enabled:hover:text-foreground disabled:opacity-40",
              pressed ? "bg-secondary text-foreground" : "text-muted-foreground",
            )}
            title={option.label}
            aria-label={option.label}
            aria-pressed={pressed}
            disabled={disabled}
            onClick={() => onChange(option.value)}
          >
            <Icon className="size-4 shrink-0" />
            <span className="hidden @7xl:inline" aria-hidden>
              {option.label}
            </span>
          </button>
        );
      })}
    </div>
  );
}

function ChromeDivider() {
  return <span className="mx-1.5 h-4 w-px bg-input" aria-hidden />;
}

// Prev / next in the stage gutters, vertically centered — next to the
// content, never on it. `onClick` undefined means "boundary reached": the
// button stays mounted but disabled, so the reader can see they are at one
// end instead of the control vanishing. `enabled:hover` so the disabled state
// gets no hover feedback.
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
      className={cn(
        "dark absolute top-1/2 flex size-10 -translate-y-1/2 items-center justify-center rounded-full bg-secondary/80 text-foreground transition-colors enabled:hover:bg-secondary disabled:opacity-30",
        side === "prev" ? "left-3" : "right-3",
      )}
      title={label}
      aria-label={label}
      disabled={!onClick}
      onClick={onClick}
    >
      <Icon className="size-5" />
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

  if (!url) {
    return (
      <div className="dark flex h-full items-center justify-center gap-2 text-body text-muted-foreground">
        <Loader2 className="size-4 animate-spin" />
        {t(($) => $.attachment.preview_loading)}
      </div>
    );
  }

  // A flex column: the canvas sizes itself with `flex: 1 1 auto` and its
  // content is absolutely positioned, so in a plain block parent it would
  // collapse to zero height and show nothing. It clips and handles its own
  // wheel events.
  return (
    <div className="flex h-full flex-col py-4">
      <ZoomCanvas
        canvas={canvas}
        content={natural}
        label={t(($) => $.image.canvas_label)}
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
    </div>
  );
}

// ---------------------------------------------------------------------------
// Dispatch
// ---------------------------------------------------------------------------

// What the top bar decided about how the file shows — see PreviewPanel.
interface StageView {
  wrap: boolean;
  htmlViewport: HtmlViewport;
  htmlSource: boolean;
  structuredView: StructuredView;
  /** The `structured` kind's parse, once its body has loaded. */
  structuredParse: ReturnType<typeof parseStructured> | null;
  renderHtmlFrame: HtmlFrameRenderer;
}

const TEXT_BACKED_KINDS: ReadonlySet<PreviewKind> = new Set<PreviewKind>([
  "markdown",
  "html",
  "table",
  "structured",
  "text",
]);

// Dispatch on PreviewKind. New cases go here; remember that the viewer frame
// (top bar, close, Download CTA, ESC handling) is shared — sub-renderers only
// own the stage. `image` is handled by PreviewPanel itself because its
// toolbar and canvas share zoom state.
//
// Layout per kind: pages (PDF, HTML, tables) fill the stage; video letterboxes
// on it; Markdown, code and data scroll on a centered sheet in the app's own
// theme; anything else that sits directly on the stage wears `dark`.
function PreviewContent({
  kind,
  source,
  state,
  view,
  onDownload,
  onBackdropClick,
}: {
  kind: Exclude<PreviewKind, "image"> | null;
  source: PreviewSource;
  state: PreviewState;
  view: StageView;
  onDownload: () => void;
  onBackdropClick: (e: React.MouseEvent) => void;
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
  if (TEXT_BACKED_KINDS.has(kind) && !state.attachmentId) {
    return (
      <UnsupportedFallback
        message={t(($) => $.attachment.preview_unsupported)}
        onDownload={onDownload}
      />
    );
  }

  const code = (text: string, language: string | undefined) => (
    <DocumentSheet width="code" onBackdropClick={onBackdropClick}>
      <CodeBlockStatic
        language={language}
        body={text}
        lineNumbers
        wrap={view.wrap}
        className="min-h-full"
      />
    </DocumentSheet>
  );

  switch (kind) {
    case "pdf":
      return (
        <div className="h-full pb-4">
          <iframe
            src={state.mediaUrl}
            className="h-full w-full rounded-lg bg-background"
            title={state.filename}
          />
        </div>
      );
    case "video":
      return (
        <div className="flex h-full w-full items-center justify-center pb-4">
          <video
            src={state.mediaUrl}
            controls
            className="h-full w-full object-contain"
          />
        </div>
      );
    case "audio":
      return (
        <div className="dark flex h-full w-full items-center justify-center p-8">
          <audio src={state.mediaUrl} controls className="w-full max-w-xl" />
        </div>
      );
    case "markdown":
      return (
        <TextBackedPreview
          attachmentId={state.attachmentId!}
          onDownload={onDownload}
          render={(text) => (
            <DocumentSheet width="prose" onBackdropClick={onBackdropClick}>
              <ReadonlyContent
                content={text}
                className="px-10 py-8"
                attachments={source.kind === "full" ? [source.attachment] : []}
              />
            </DocumentSheet>
          )}
        />
      );
    case "html":
      return (
        <TextBackedPreview
          attachmentId={state.attachmentId!}
          onDownload={onDownload}
          render={(text) =>
            view.htmlSource ? (
              code(text, "xml")
            ) : (
              <HtmlViewportFrame viewport={view.htmlViewport}>
                {view.renderHtmlFrame({ html: text, title: state.filename })}
              </HtmlViewportFrame>
            )
          }
        />
      );
    case "table":
      return (
        <TextBackedPreview
          attachmentId={state.attachmentId!}
          onDownload={onDownload}
          render={(text) => (
            // Keyed on the file: sort order and column widths belong to it.
            <TablePreview
              key={state.attachmentId}
              text={text}
              delimiter={tableDelimiter(state.contentType, state.filename)}
            />
          )}
        />
      );
    case "structured":
      return (
        <TextBackedPreview
          attachmentId={state.attachmentId!}
          onDownload={onDownload}
          render={(text) => {
            const parse = view.structuredParse;
            if (parse?.ok && view.structuredView === "tree") {
              return (
                <DocumentSheet width="code" onBackdropClick={onBackdropClick}>
                  <div className="px-4 py-4">
                    <StructuredTree key={state.attachmentId} value={parse.value} />
                  </div>
                </DocumentSheet>
              );
            }
            return (
              <div className="flex h-full flex-col">
                {parse && !parse.ok && (
                  <p
                    className="dark mx-auto mb-2 w-full max-w-5xl shrink-0 text-body text-muted-foreground"
                    role="status"
                  >
                    {t(($) => $.attachment.structured_parse_failed)}{" "}
                    <span className="font-mono text-caption">{parse.message}</span>
                  </p>
                )}
                <div className="min-h-0 flex-1">
                  {code(text, extensionToLanguage(state.filename))}
                </div>
              </div>
            );
          }}
        />
      );
    case "text":
      return (
        <TextBackedPreview
          attachmentId={state.attachmentId!}
          onDownload={onDownload}
          render={(text) => code(text, extensionToLanguage(state.filename))}
        />
      );
  }
}

// A read-not-looked-at document: a centered sheet in the app's theme that
// scrolls with the stage. Prose keeps a reading measure; code gets the room
// long lines need. The scroll container spans the whole stage, so the wheel
// works anywhere and its empty sides still count as backdrop.
function DocumentSheet({
  width,
  onBackdropClick,
  children,
}: {
  width: "prose" | "code";
  onBackdropClick: (e: React.MouseEvent) => void;
  children: ReactNode;
}) {
  return (
    <div className="h-full overflow-y-auto" onClick={onBackdropClick}>
      <div
        className={cn(
          "mx-auto min-h-full w-full rounded-t-lg bg-background text-foreground",
          width === "prose" ? "max-w-3xl" : "max-w-5xl",
        )}
      >
        {children}
      </div>
    </div>
  );
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
      <div className="dark flex h-full items-center justify-center gap-2 text-body text-muted-foreground">
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
// Fallback — used for 413 / 415 / unknown kinds. Sits on the stage, so dark.
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
    <div className="dark flex h-full flex-col items-center justify-center gap-3 px-8 text-center">
      <FileText className="size-8 text-muted-foreground" />
      <p className="text-body text-muted-foreground">{message}</p>
      <button
        type="button"
        className="inline-flex items-center gap-2 rounded-md border border-input bg-secondary px-3 py-1.5 text-body text-foreground transition-colors hover:bg-muted"
        onClick={onDownload}
      >
        <Download className="size-4" />
        {t(($) => $.image.download)}
      </button>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Standalone — the viewer's top bar and stage as a page of their own
// ---------------------------------------------------------------------------

/**
 * The "Open in new tab" destination: the same bar and stage as the viewer,
 * on the same dark ground, filling its host instead of overlaying the app.
 * No close, sequence or window drag region — the tab is the frame.
 */
export function AttachmentPreviewStandalone({
  attachment,
  renderHtmlFrame,
}: {
  attachment: Attachment;
  renderHtmlFrame?: HtmlFrameRenderer;
}) {
  const download = useDownloadAttachment();
  const source = useMemo<PreviewSource>(
    () => ({ kind: "full", attachment }),
    [attachment],
  );
  const state = normalize(source);

  return (
    <div className="flex h-full w-full flex-col bg-black/95">
      <PreviewPanel
        surface="page"
        kind={state.kind}
        source={source}
        state={state}
        onDownload={() => download(attachment.id)}
        reduceMotion
        renderHtmlFrame={renderHtmlFrame}
      />
    </div>
  );
}

// Re-export the predicate from the dispatch util so entry-point components
// only need a single import to gate the Eye button.
export { isPreviewable } from "./utils/preview";
