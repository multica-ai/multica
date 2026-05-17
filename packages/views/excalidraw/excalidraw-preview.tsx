"use client";

/**
 * ExcalidrawPreview — inline SVG render of an `.excalidraw` attachment.
 *
 * Read-only. Loads the Excalidraw `exportToSvg` helper lazily so the heavy
 * editor bundle (~1MB) never lands on issue pages that don't carry a diagram.
 *
 * Fetches the JSON body through the auth'd `/api/attachments/{id}/content`
 * proxy (signed CloudFront URLs aren't CORS-safe for client-side reads).
 * Parses defensively — a malformed body downgrades to a file-link fallback
 * instead of a render-time exception.
 *
 * Click-to-expand: opens the shared AttachmentPreviewModal with kind
 * "excalidraw"; the modal mounts the same component in `expanded` mode so
 * the inline + expanded views share the React Query cache for the JSON body.
 */

import {
  Suspense,
  lazy,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useQuery } from "@tanstack/react-query";
import { Download, FileText, Loader2, Maximize2 } from "lucide-react";
import {
  api,
  PreviewTooLargeError,
  PreviewUnsupportedError,
} from "@multica/core/api";
import type { Attachment } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import { openExternal } from "../platform";

type ExportToSvgFn = (typeof import("@excalidraw/excalidraw"))["exportToSvg"];

let exportToSvgPromise: Promise<ExportToSvgFn> | null = null;

// Single shared dynamic import keeps subsequent mounts cheap and the bundle
// out of the initial chunk graph.
function loadExportToSvg(): Promise<ExportToSvgFn> {
  if (!exportToSvgPromise) {
    exportToSvgPromise = import("@excalidraw/excalidraw").then(
      (mod) => mod.exportToSvg,
    );
  }
  return exportToSvgPromise;
}

// Defensive parser — anything that doesn't look like an Excalidraw scene
// returns null so the renderer can show the fallback link.
interface ParsedScene {
  elements: readonly unknown[];
  appState: Record<string, unknown>;
  files: Record<string, unknown>;
}

function parseScene(text: string): ParsedScene | null {
  try {
    const data = JSON.parse(text);
    if (!data || typeof data !== "object") return null;
    if (!Array.isArray((data as { elements?: unknown }).elements)) return null;
    return {
      elements: (data as { elements: readonly unknown[] }).elements,
      appState:
        ((data as { appState?: Record<string, unknown> }).appState as Record<
          string,
          unknown
        >) ?? {},
      files:
        ((data as { files?: Record<string, unknown> }).files as Record<
          string,
          unknown
        >) ?? {},
    };
  } catch {
    return null;
  }
}

// ---------------------------------------------------------------------------
// Props
// ---------------------------------------------------------------------------

export interface ExcalidrawPreviewProps {
  /** Resolved attachment. Required to hit the ID-keyed text-content proxy. */
  attachment: Attachment;
  /** When true: no max-height, no expand button (used inside the modal). */
  expanded?: boolean;
  /** Optional click-to-expand handler. Inline mode only. */
  onExpand?: () => void;
}

export function ExcalidrawPreview({
  attachment,
  expanded = false,
  onExpand,
}: ExcalidrawPreviewProps) {
  const { t } = useT("editor");

  // Cache keyed on attachment id alone — the inline preview and the modal
  // render the same component and share this entry.
  const query = useQuery({
    queryKey: ["attachment-content", attachment.id] as const,
    queryFn: () => api.getAttachmentTextContent(attachment.id),
    retry: false,
    staleTime: 5 * 60_000,
    gcTime: 30 * 60_000,
  });

  const scene = useMemo(() => {
    if (!query.data) return null;
    return parseScene(query.data.text);
  }, [query.data]);

  const handleDownloadFallback = () => {
    openExternal(attachment.download_url);
  };

  if (query.isLoading) {
    return (
      <Frame expanded={expanded}>
        <div
          className="flex h-40 items-center justify-center gap-2 text-sm text-muted-foreground"
          role="status"
          aria-label={t(($) => $.attachment.preview_loading)}
        >
          <Loader2 className="size-4 animate-spin" />
          {t(($) => $.attachment.preview_loading)}
        </div>
      </Frame>
    );
  }

  if (query.error) {
    const message =
      query.error instanceof PreviewTooLargeError
        ? t(($) => $.attachment.preview_too_large)
        : query.error instanceof PreviewUnsupportedError
          ? t(($) => $.attachment.preview_unsupported)
          : t(($) => $.attachment.preview_failed);
    return (
      <FallbackLink
        attachment={attachment}
        message={message}
        onOpen={handleDownloadFallback}
      />
    );
  }

  if (!scene) {
    return (
      <FallbackLink
        attachment={attachment}
        message={t(($) => $.excalidraw.invalid_scene)}
        onOpen={handleDownloadFallback}
      />
    );
  }

  return (
    <Suspense fallback={<LoadingFrame expanded={expanded} label={t(($) => $.attachment.preview_loading)} />}>
      <ExcalidrawSvg
        scene={scene}
        expanded={expanded}
        onExpand={onExpand}
        ariaLabel={attachment.filename}
        expandLabel={t(($) => $.excalidraw.expand)}
      />
    </Suspense>
  );
}

// ---------------------------------------------------------------------------
// SVG renderer — uses Suspense so the dynamic import only blocks once
// ---------------------------------------------------------------------------

// React.lazy needs a module-shaped default export. Wrap a tiny component
// that consumes the resolved exportToSvg from the shared promise.
const ExcalidrawSvg = lazy(async () => {
  const exportToSvg = await loadExportToSvg();
  function Inner({
    scene,
    expanded,
    onExpand,
    ariaLabel,
    expandLabel,
  }: {
    scene: ParsedScene;
    expanded: boolean;
    onExpand?: () => void;
    ariaLabel: string;
    expandLabel: string;
  }) {
    const containerRef = useRef<HTMLDivElement | null>(null);
    const [renderError, setRenderError] = useState(false);

    useEffect(() => {
      let cancelled = false;
      const host = containerRef.current;
      if (!host) return;

      (async () => {
        try {
          // Defensive parse keeps us outside Excalidraw's nominal types;
          // exportToSvg validates the shapes itself and a render-time
          // exception is caught below.
          const svg = await exportToSvg({
            elements: scene.elements as Parameters<ExportToSvgFn>[0]["elements"],
            appState: scene.appState as Parameters<ExportToSvgFn>[0]["appState"],
            files: scene.files as Parameters<ExportToSvgFn>[0]["files"],
            exportPadding: 16,
          });
          if (cancelled) return;
          // Let CSS govern sizing — the raw SVG carries fixed width/height
          // attributes that would otherwise overflow the frame.
          svg.removeAttribute("width");
          svg.removeAttribute("height");
          svg.setAttribute("preserveAspectRatio", "xMidYMid meet");
          svg.style.maxWidth = "100%";
          svg.style.height = expanded ? "100%" : "auto";
          svg.style.display = "block";
          host.replaceChildren(svg);
        } catch {
          if (!cancelled) setRenderError(true);
        }
      })();

      return () => {
        cancelled = true;
      };
    }, [scene, expanded]);

    if (renderError) {
      return (
        <Frame expanded={expanded}>
          <div className="p-4 text-sm text-muted-foreground">{expandLabel}</div>
        </Frame>
      );
    }

    const viewBackground =
      typeof scene.appState.viewBackgroundColor === "string"
        ? (scene.appState.viewBackgroundColor as string)
        : undefined;

    return (
      <Frame
        expanded={expanded}
        background={viewBackground}
        onClick={!expanded && onExpand ? onExpand : undefined}
      >
        <div
          ref={containerRef}
          className={cn(
            "flex w-full",
            expanded ? "h-full items-center justify-center" : "items-center justify-center",
          )}
          role="img"
          aria-label={ariaLabel}
        />
        {!expanded && onExpand && (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              onExpand();
            }}
            className="absolute right-2 top-2 inline-flex items-center gap-1 rounded-md border border-border bg-background/90 px-2 py-1 text-xs text-muted-foreground shadow-sm transition-colors hover:bg-background hover:text-foreground"
            aria-label={expandLabel}
            title={expandLabel}
          >
            <Maximize2 className="size-3" />
            {expandLabel}
          </button>
        )}
      </Frame>
    );
  }
  return { default: Inner };
});

// ---------------------------------------------------------------------------
// Visual frame
// ---------------------------------------------------------------------------

function Frame({
  children,
  expanded,
  background,
  onClick,
}: {
  children: ReactNode;
  expanded: boolean;
  background?: string;
  onClick?: () => void;
}) {
  return (
    <div
      className={cn(
        "relative my-2 overflow-hidden rounded-md border border-border",
        expanded ? "h-full w-full" : "max-h-[600px] cursor-default",
        onClick && "cursor-zoom-in",
      )}
      style={background ? { backgroundColor: background } : undefined}
      onClick={onClick}
    >
      {children}
    </div>
  );
}

function LoadingFrame({ expanded, label }: { expanded: boolean; label: string }) {
  return (
    <Frame expanded={expanded}>
      <div
        className="flex h-40 items-center justify-center gap-2 text-sm text-muted-foreground"
        role="status"
        aria-label={label}
      >
        <Loader2 className="size-4 animate-spin" />
        {label}
      </div>
    </Frame>
  );
}

// ---------------------------------------------------------------------------
// Fallback — broken JSON or proxy refused; surface a download affordance
// ---------------------------------------------------------------------------

function FallbackLink({
  attachment,
  message,
  onOpen,
}: {
  attachment: Attachment;
  message: string;
  onOpen: () => void;
}) {
  return (
    <div className="my-2 flex flex-col items-start gap-2 rounded-md border border-border bg-muted/40 p-3 text-sm">
      <div className="flex items-center gap-2 text-muted-foreground">
        <FileText className="size-4 shrink-0" />
        <span className="truncate">{attachment.filename}</span>
      </div>
      <p className="text-xs text-muted-foreground">{message}</p>
      <button
        type="button"
        onClick={onOpen}
        className="inline-flex items-center gap-1 rounded-md border border-border bg-background px-2 py-1 text-xs transition-colors hover:bg-muted"
      >
        <Download className="size-3" />
        {attachment.filename}
      </button>
    </div>
  );
}
