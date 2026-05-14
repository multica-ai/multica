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
 */

import { useEffect, useId, useMemo, useRef, useState, type CSSProperties } from "react";
import { createPortal } from "react-dom";
import { Maximize2 } from "lucide-react";
import { useT } from "../i18n";

type MermaidAPI = typeof import("mermaid").default;

type MermaidLayout = {
  width?: number;
  height?: number;
};

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

function getMermaidLayout(svg: string): MermaidLayout {
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

  return {};
}

// Default skeleton height while Mermaid loads + renders for the first time
// in this session. Picked to absorb most issue-detail diagrams without
// excessive empty space; web.dev's CLS guidance recommends reserving any
// such space upfront so async content doesn't shift surrounding layout.
const MERMAID_SKELETON_HEIGHT_PX = 280;
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

function readCachedLayout(chart: string): MermaidLayout | null {
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

function writeCachedLayout(chart: string, layout: MermaidLayout): void {
  if (typeof window === "undefined") return;
  if (!layout.width || !layout.height) return;
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

function buildExpandedMermaidDocument(svg: string, host: HTMLElement | null): string {
  const cssVariables = getSandboxCssVariables(host);

  return `<!doctype html><html><head><style>:root { ${cssVariables} } html, body { width: 100%; height: 100%; } body { margin: 0; display: flex; align-items: center; justify-content: center; background: transparent; } svg { max-width: 100%; max-height: 100%; width: auto; height: auto; }</style></head><body>${svg}</body></html>`;
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

function MermaidLightbox({
  srcDoc,
  onClose,
}: {
  srcDoc: string;
  onClose: () => void;
}) {
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [onClose]);

  return createPortal(
    <div
      className="mermaid-diagram-lightbox"
      role="dialog"
      aria-modal="true"
      aria-label="Mermaid diagram fullscreen view"
      onClick={onClose}
    >
      <iframe
        className="mermaid-diagram-lightbox-frame"
        sandbox=""
        srcDoc={srcDoc}
        title="Mermaid diagram fullscreen"
        onClick={(e) => e.stopPropagation()}
      />
    </div>,
    document.body,
  );
}

export function MermaidDiagram({ chart }: { chart: string }) {
  const { t } = useT("editor");
  const reactId = useId();
  const containerRef = useRef<HTMLDivElement>(null);
  const diagramId = useMemo(
    () => `mermaid-${reactId.replace(/[^a-zA-Z0-9_-]/g, "")}`,
    [reactId],
  );
  const themeVersion = useThemeVersion();
  const [sandboxedDocument, setSandboxedDocument] = useState<string | null>(null);
  const [expandedDocument, setExpandedDocument] = useState<string | null>(null);
  // Lazy initial value: if we've rendered this exact chart already in the
  // current session, the cached layout lets us reserve correct space on the
  // very first paint — eliminating the 0px → real-height shift that breaks
  // deep-link scroll positioning and ambient reading position.
  const [layout, setLayout] = useState<MermaidLayout>(() => readCachedLayout(chart) ?? {});
  const [error, setError] = useState<string | null>(null);
  const [lightboxOpen, setLightboxOpen] = useState(false);

  useEffect(() => {
    let cancelled = false;

    async function renderDiagram() {
      try {
        setError(null);
        setSandboxedDocument(null);
        setExpandedDocument(null);
        // Seed layout from cache (if any) so the skeleton sizes correctly
        // even when `chart` changes after mount — the lazy useState above
        // only fires once.
        setLayout(readCachedLayout(chart) ?? {});
        const mermaid = await getMermaid();
        mermaid.initialize({
          startOnLoad: false,
          securityLevel: "strict",
          theme: "base",
          themeVariables: getMermaidThemeVariables(containerRef.current),
        });
        const { svg: renderedSvg } = await mermaid.render(diagramId, chart);
        if (!cancelled) {
          const measured = getMermaidLayout(renderedSvg);
          setLayout(measured);
          writeCachedLayout(chart, measured);
          setSandboxedDocument(
            buildSandboxedMermaidDocument(renderedSvg, containerRef.current),
          );
          setExpandedDocument(
            buildExpandedMermaidDocument(renderedSvg, containerRef.current),
          );
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : "Failed to render Mermaid diagram");
        }
      }
    }

    void renderDiagram();

    return () => {
      cancelled = true;
    };
  }, [chart, diagramId, themeVersion]);

  if (error) {
    return (
      <div ref={containerRef} className="mermaid-diagram mermaid-diagram-error">
        <p>{t(($) => $.mermaid.render_error)}</p>
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
  const containerStyle: CSSProperties | undefined = sandboxedDocument
    ? undefined
    : { minHeight: layout.height ?? MERMAID_SKELETON_HEIGHT_PX };

  return (
    <div
      ref={containerRef}
      className="mermaid-diagram"
      aria-label="Mermaid diagram"
      style={containerStyle}
    >
      {sandboxedDocument ? (
        <>
          <iframe
            className="mermaid-diagram-frame"
            sandbox=""
            srcDoc={sandboxedDocument}
            style={{
              height: layout.height ? `${layout.height}px` : undefined,
              width: layout.width ? `${layout.width}px` : undefined,
            }}
            title="Mermaid diagram"
          />
          <div className="mermaid-diagram-toolbar">
            <button
              type="button"
              onClick={() => setLightboxOpen(true)}
              title="Open fullscreen"
              aria-label="Open Mermaid diagram fullscreen"
            >
              <Maximize2 className="size-3.5" />
            </button>
          </div>
          {lightboxOpen && expandedDocument && (
            <MermaidLightbox
              srcDoc={expandedDocument}
              onClose={() => setLightboxOpen(false)}
            />
          )}
        </>
      ) : (
        <div className="mermaid-diagram-loading">{t(($) => $.mermaid.rendering)}</div>
      )}
    </div>
  );
}
