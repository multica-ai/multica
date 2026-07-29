"use client";

/**
 * MermaidDiagram — sandboxed Mermaid diagram renderer.
 *
 * Extracted from `readonly-content.tsx` so the Tiptap CodeBlock NodeView
 * (`code-block-view.tsx`) can render the same component when a code block's
 * language is `mermaid`. Previously Mermaid only worked in read-only
 * markdown surfaces (comment cards) — issue descriptions, which always
 * stay in the Tiptap editor, never rendered diagrams.
 *
 * Theme variables are detected from the host's CSS custom properties so the
 * diagram colors match light/dark mode. The SVG is rendered inside a
 * sandboxed iframe to keep Mermaid's runtime stylesheet from leaking into
 * the page.
 *
 * This component owns the inline presentation; the full-screen experience
 * lives in `MermaidViewer`. Both read the same rendered SVG, so what you
 * export or blow up is always what you were looking at.
 */

import {
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
} from "react";
import { Check, Copy, Maximize2 } from "lucide-react";
import { copyText } from "@multica/ui/lib/clipboard";
import { useT } from "../i18n";
import { useDragToScroll } from "./hooks/use-drag-to-scroll";
import { MermaidViewer } from "./mermaid-viewer";
import type { Size } from "./utils/diagram-transform";

type MermaidAPI = typeof import("mermaid").default;

let mermaidPromise: Promise<MermaidAPI> | null = null;

function getMermaid(): Promise<MermaidAPI> {
  mermaidPromise ??= import("mermaid").then(({ default: mermaid }) => mermaid);

  return mermaidPromise;
}

function toLegacyColor(color: string, fallback: string, ownerDocument: Document): string {
  const canvas = ownerDocument.createElement("canvas");
  canvas.width = 1;
  canvas.height = 1;
  const context = canvas.getContext("2d", { willReadFrequently: true });
  if (!context) return fallback;

  // Mermaid's color parser only supports legacy color syntax. Canvas can parse
  // modern CSS Color 4 values such as oklch(), then getImageData gives concrete
  // 8-bit sRGB bytes that Mermaid can consume safely.
  context.fillStyle = "#000";
  context.fillStyle = color || fallback;
  context.fillRect(0, 0, 1, 1);
  const [red, green, blue] = context.getImageData(0, 0, 1, 1).data;

  return `rgb(${red}, ${green}, ${blue})`;
}

function resolveCssColor(
  host: HTMLElement,
  variableName: string,
  fallback: string,
): string {
  const probe = host.ownerDocument.createElement("span");
  probe.style.color = `var(${variableName})`;
  probe.style.display = "none";
  host.appendChild(probe);
  const color = getComputedStyle(probe).color;
  probe.remove();

  return toLegacyColor(color || fallback, fallback, host.ownerDocument);
}

const FALLBACK_BACKGROUND = "rgb(255, 255, 255)";

function getMermaidThemeVariables(host: HTMLElement | null) {
  if (!host) {
    return {
      primaryColor: "rgb(245, 245, 245)",
      primaryBorderColor: "rgb(59, 130, 246)",
      primaryTextColor: "rgb(17, 24, 39)",
      lineColor: "rgb(107, 114, 128)",
      fontFamily: "inherit",
    };
  }

  return {
    primaryColor: resolveCssColor(host, "--muted", "rgb(245, 245, 245)"),
    primaryBorderColor: resolveCssColor(host, "--primary", "rgb(59, 130, 246)"),
    primaryTextColor: resolveCssColor(host, "--foreground", "rgb(17, 24, 39)"),
    lineColor: resolveCssColor(host, "--muted-foreground", "rgb(107, 114, 128)"),
    fontFamily: "inherit",
  };
}

function getSandboxCssVariables(host: HTMLElement | null): string {
  const styles = host ? getComputedStyle(host) : null;
  return ["--muted", "--primary", "--foreground", "--muted-foreground"]
    .map((name) => `${name}: ${styles?.getPropertyValue(name).trim() || "initial"};`)
    .join(" ");
}

/**
 * Concrete colors for export. An exported file has no host page to inherit
 * from, so `var(--muted)` / `font-family: inherit` would resolve to nothing.
 */
function getExportStyle(host: HTMLElement | null): {
  background: string;
  fontFamily: string;
} {
  if (!host) {
    return { background: FALLBACK_BACKGROUND, fontFamily: "sans-serif" };
  }

  return {
    background: resolveCssColor(host, "--muted", FALLBACK_BACKGROUND),
    fontFamily: getComputedStyle(host).fontFamily || "sans-serif",
  };
}

function getMermaidLayout(svg: string): Size | null {
  const viewBoxMatch = svg.match(
    /viewBox=["']\s*([\d.-]+)\s+([\d.-]+)\s+([\d.-]+)\s+([\d.-]+)\s*["']/i,
  );
  const [, , , widthValue, heightValue] = viewBoxMatch ?? [];
  const width = widthValue ? Number.parseFloat(widthValue) : undefined;
  const height = heightValue ? Number.parseFloat(heightValue) : undefined;

  if (width && height && width > 0 && height > 0) {
    return {
      width: Math.ceil(width),
      height: Math.ceil(height),
    };
  }

  return null;
}

// Default skeleton height while Mermaid loads + renders for the first time
// in this session. Picked to absorb most issue-detail diagrams without
// excessive empty space; web.dev's CLS guidance recommends reserving any
// such space upfront so async content doesn't shift surrounding layout.
export const MERMAID_SKELETON_HEIGHT_PX = 280;
const MERMAID_LAYOUT_CACHE_PREFIX = "multica:mermaid:layout:";

// DJB2 — small, fast, sufficient for sessionStorage cache keys. The chart
// text itself is too unwieldy as a key (length, special chars), and a
// crypto-strength hash would have to be async.
function hashChart(chart: string): string {
  let hash = 5381;
  for (let i = 0; i < chart.length; i++) {
    hash = ((hash << 5) + hash) ^ chart.charCodeAt(i);
  }
  return (hash >>> 0).toString(36);
}

/**
 * Height to reserve for a diagram that has not rendered yet: the real height
 * when this exact chart already rendered in this session, otherwise the
 * skeleton default. Exported so the near-viewport lazy shell
 * (rich-content/lazy-rich-block.tsx) reserves the SAME space this component
 * would, instead of maintaining a second guess at the size.
 *
 * NOT safe to call during render: it reads sessionStorage, which does not exist
 * on the server, so the value differs between the server frame and the browser's
 * hydration frame whenever the cache is warm. Callers must use the skeleton
 * default for the first frame and call this from an effect (see
 * useReservedMermaidHeightPx in rich-content/rich-code-block.tsx).
 */
export function reservedMermaidHeightPx(chart: string): number {
  return readCachedLayout(chart)?.height ?? MERMAID_SKELETON_HEIGHT_PX;
}

function readCachedLayout(chart: string): Size | null {
  if (typeof window === "undefined") return null;
  try {
    const raw = window.sessionStorage.getItem(
      MERMAID_LAYOUT_CACHE_PREFIX + hashChart(chart),
    );
    if (!raw) return null;
    const parsed = JSON.parse(raw);
    if (
      typeof parsed?.width === "number" &&
      typeof parsed?.height === "number" &&
      parsed.width > 0 &&
      parsed.height > 0
    ) {
      return { width: parsed.width, height: parsed.height };
    }
    return null;
  } catch {
    return null;
  }
}

function writeCachedLayout(chart: string, layout: Size | null): void {
  if (typeof window === "undefined") return;
  if (!layout) return;
  try {
    window.sessionStorage.setItem(
      MERMAID_LAYOUT_CACHE_PREFIX + hashChart(chart),
      JSON.stringify({ width: layout.width, height: layout.height }),
    );
  } catch {
    // Quota exceeded or storage disabled — degrade silently; we still
    // render correctly, just without the zero-shift optimisation.
  }
}

function buildSandboxedMermaidDocument(svg: string, host: HTMLElement | null): string {
  const cssVariables = getSandboxCssVariables(host);

  return `<!doctype html><html><head><style>:root { ${cssVariables} } body { margin: 0; display: flex; justify-content: center; background: transparent; } svg { max-width: 100%; height: auto; }</style></head><body>${svg}</body></html>`;
}

/**
 * Viewer document: the diagram is drawn at natural size and the host applies
 * zoom as a CSS transform on the wrapper. Clamping to `max-width: 100%` here
 * (as the inline document does) would fight the transform and cap zoom at
 * whatever the iframe happens to measure.
 */
function buildViewerMermaidDocument(
  svg: string,
  host: HTMLElement | null,
  layout: Size,
): string {
  const cssVariables = getSandboxCssVariables(host);

  return `<!doctype html><html><head><style>:root { ${cssVariables} } html, body { margin: 0; width: 100%; height: 100%; overflow: hidden; background: transparent; } svg { display: block; width: ${layout.width}px; height: ${layout.height}px; max-width: none; }</style></head><body>${svg}</body></html>`;
}

function useThemeVersion() {
  const [themeVersion, setThemeVersion] = useState(0);

  useEffect(() => {
    const bumpThemeVersion = () => setThemeVersion((version) => version + 1);
    const observer = new MutationObserver(bumpThemeVersion);
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class", "style", "data-theme"],
    });
    if (document.body) {
      observer.observe(document.body, {
        attributes: true,
        attributeFilter: ["class", "style", "data-theme"],
      });
    }

    const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
    mediaQuery.addEventListener("change", bumpThemeVersion);

    return () => {
      observer.disconnect();
      mediaQuery.removeEventListener("change", bumpThemeVersion);
    };
  }, []);

  return themeVersion;
}

/**
 * Tracks which horizontal edges of a scroll container have content beyond
 * them, so CSS can fade those edges as an affordance that the diagram
 * continues off-screen.
 */
function useHorizontalOverflow(ref: React.RefObject<HTMLElement | null>, deps: unknown[]) {
  const [edges, setEdges] = useState<{ start: boolean; end: boolean }>({
    start: false,
    end: false,
  });

  useEffect(() => {
    const element = ref.current;
    if (!element) return;

    const measure = () => {
      const { scrollLeft, scrollWidth, clientWidth } = element;
      const maxScroll = scrollWidth - clientWidth;
      setEdges((previous) => {
        // 1px tolerance: fractional layout widths otherwise leave a fade
        // permanently stuck on at rest.
        const start = scrollLeft > 1;
        const end = scrollLeft < maxScroll - 1;
        return previous.start === start && previous.end === end
          ? previous
          : { start, end };
      });
    };

    measure();
    element.addEventListener("scroll", measure, { passive: true });

    const observer =
      typeof ResizeObserver === "undefined" ? null : new ResizeObserver(measure);
    observer?.observe(element);

    return () => {
      element.removeEventListener("scroll", measure);
      observer?.disconnect();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ref, ...deps]);

  return edges;
}

// Size the viewer falls back to when the SVG carries no usable viewBox. Mermaid
// always emits one, but the viewer's transform math needs a concrete content
// size, and rendering nothing at all would be a worse failure than an
// approximate one.
const VIEWER_FALLBACK_LAYOUT: Size = { width: 800, height: MERMAID_SKELETON_HEIGHT_PX };

interface RenderedDiagram {
  svg: string;
  inlineDocument: string;
  viewerDocument: string;
  /** Natural size, or null when the viewBox was unreadable. Sizes the inline iframe. */
  layout: Size | null;
  /** Always concrete — the viewer cannot lay out against an unknown size. */
  viewerLayout: Size;
  exportBackground: string;
  exportFontFamily: string;
}

export function MermaidDiagram({ chart }: { chart: string }) {
  const { t } = useT("editor");
  const reactId = useId();
  const containerRef = useRef<HTMLDivElement>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const expandButtonRef = useRef<HTMLButtonElement>(null);
  const diagramId = useMemo(
    () => `mermaid-${reactId.replace(/[^a-zA-Z0-9_-]/g, "")}`,
    [reactId],
  );
  const themeVersion = useThemeVersion();
  // One object rather than parallel states: a half-applied re-render (new SVG,
  // old layout) would size the viewer's iframe against the wrong diagram.
  const [rendered, setRendered] = useState<RenderedDiagram | null>(null);
  // Lazy initial value: if we've rendered this exact chart already in the
  // current session, the cached layout lets us reserve correct space on the
  // very first paint — eliminating the 0px → real-height shift that breaks
  // deep-link scroll positioning and ambient reading position.
  const [skeletonLayout, setSkeletonLayout] = useState<Size | null>(() =>
    readCachedLayout(chart),
  );
  const [error, setError] = useState<string | null>(null);
  const [viewerOpen, setViewerOpen] = useState(false);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    let cancelled = false;

    async function renderDiagram() {
      try {
        setError(null);
        // Deliberately NOT clearing `rendered` here. A theme switch re-runs
        // this effect, and dropping to null would unmount the open viewer —
        // closing it, and with it the user's zoom and position. Keeping the
        // previous diagram on screen until the new one is ready also removes
        // a flash of the loading skeleton on every theme toggle.
        setSkeletonLayout(readCachedLayout(chart));
        const mermaid = await getMermaid();
        mermaid.initialize({
          startOnLoad: false,
          securityLevel: "strict",
          theme: "base",
          // Render labels as SVG <text> instead of Mermaid's default HTML-in-
          // <foreignObject>. Browsers do not rasterize foreignObject when an
          // SVG is drawn through an <img> — verified in Chromium, the label
          // paints zero pixels AND taints the canvas, so PNG export produces
          // nothing at all. SVG text keeps the export self-contained.
          htmlLabels: false,
          themeVariables: getMermaidThemeVariables(containerRef.current),
          // On invalid syntax, make render() throw instead of drawing Mermaid's
          // built-in error graphic into the DOM. The catch below then shows our
          // own compact error state — no orphaned error SVG, and no extra parse
          // pass over valid charts.
          suppressErrorRendering: true,
        });
        const { svg: renderedSvg } = await mermaid.render(diagramId, chart);
        if (cancelled) return;

        const measured = getMermaidLayout(renderedSvg);
        const viewerLayout = measured ?? VIEWER_FALLBACK_LAYOUT;
        const exportStyle = getExportStyle(containerRef.current);
        writeCachedLayout(chart, measured);
        setSkeletonLayout(measured);
        setRendered({
          svg: renderedSvg,
          inlineDocument: buildSandboxedMermaidDocument(renderedSvg, containerRef.current),
          // Same size the viewer lays out against, so the drawn SVG and the
          // transform never disagree about how big the diagram is.
          viewerDocument: buildViewerMermaidDocument(
            renderedSvg,
            containerRef.current,
            viewerLayout,
          ),
          layout: measured,
          viewerLayout,
          exportBackground: exportStyle.background,
          exportFontFamily: exportStyle.fontFamily,
        });
      } catch (err) {
        if (!cancelled) {
          setRendered(null);
          setError(err instanceof Error ? err.message : "Failed to render Mermaid diagram");
        }
      }
    }

    void renderDiagram();

    return () => {
      cancelled = true;
    };
  }, [chart, diagramId, themeVersion]);

  const overflow = useHorizontalOverflow(scrollRef, [rendered?.inlineDocument]);
  const openViewer = useCallback(() => setViewerOpen(true), []);
  const dragToScroll = useDragToScroll({ onTap: openViewer });

  const handleCopySource = useCallback(async () => {
    if (await copyText(chart)) {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  }, [chart]);

  if (error) {
    return (
      <div ref={containerRef} className="mermaid-diagram mermaid-diagram-error">
        <div className="mermaid-diagram-error-head">
          <p>{t(($) => $.mermaid.render_error)}</p>
          <button
            type="button"
            onClick={handleCopySource}
            title={t(($) => $.mermaid.copy_source)}
            aria-label={t(($) => $.mermaid.copy_source)}
          >
            {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
          </button>
        </div>
        {/* The parser message is the only clue about which line is wrong;
            without it the fallback is just an unexplained code block. */}
        <p className="mermaid-diagram-error-detail">{error}</p>
        <pre>
          <code>{chart}</code>
        </pre>
      </div>
    );
  }

  // While the iframe is not yet ready, hold the container at the skeleton
  // height (cached real height when available, fallback default otherwise).
  // Once the iframe renders, drop the min-height — the iframe's own height
  // drives layout. If the cache was right, this transition is zero-shift.
  const containerStyle: CSSProperties | undefined = rendered
    ? undefined
    : { minHeight: skeletonLayout?.height ?? MERMAID_SKELETON_HEIGHT_PX };

  return (
    <div
      ref={containerRef}
      className="mermaid-diagram"
      aria-label="Mermaid diagram"
      style={containerStyle}
      data-overflow-start={overflow.start ? "" : undefined}
      data-overflow-end={overflow.end ? "" : undefined}
    >
      {rendered ? (
        <>
          {/* The scroll container is a sibling of the toolbar, not its parent:
              as an absolutely-positioned child of the scroller the toolbar used
              to slide out of view with the diagram on wide charts. */}
          {/* Tap opens the viewer, but a drag must not — see useDragToScroll.
              The gesture drives this instead of `onClick`, because a click
              fires at the end of a drag too and would reopen the viewer over
              a user who was only trying to look at the rest of a wide chart. */}
          <div
            ref={scrollRef}
            className="mermaid-diagram-scroll"
            {...dragToScroll}
          >
            <iframe
              className="mermaid-diagram-frame"
              sandbox=""
              srcDoc={rendered.inlineDocument}
              style={{
                height: rendered.layout ? `${rendered.layout.height}px` : undefined,
                width: rendered.layout ? `${rendered.layout.width}px` : undefined,
              }}
              title="Mermaid diagram"
            />
          </div>
          <div className="mermaid-diagram-toolbar">
            <button
              type="button"
              onClick={handleCopySource}
              title={t(($) => $.mermaid.copy_source)}
              aria-label={t(($) => $.mermaid.copy_source)}
            >
              {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
            </button>
            <button
              ref={expandButtonRef}
              type="button"
              onClick={() => setViewerOpen(true)}
              title={t(($) => $.mermaid.open_viewer)}
              aria-label={t(($) => $.mermaid.open_viewer)}
            >
              <Maximize2 className="size-3.5" />
            </button>
          </div>
          <MermaidViewer
            open={viewerOpen}
            onOpenChange={setViewerOpen}
            chart={chart}
            svg={rendered.svg}
            viewerDocument={rendered.viewerDocument}
            layout={rendered.viewerLayout}
            exportBackground={rendered.exportBackground}
            exportFontFamily={rendered.exportFontFamily}
            finalFocusRef={expandButtonRef}
          />
        </>
      ) : (
        <div className="mermaid-diagram-loading">{t(($) => $.mermaid.rendering)}</div>
      )}
    </div>
  );
}
