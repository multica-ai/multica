"use client";

/**
 * HtmlViewportFrame — lays an HTML attachment's iframe out at a chosen
 * viewport width, for the viewer's html kind.
 *
 *   - fill    : "Fit". The iframe takes the whole stage, as the page would in
 *               a browser window that size. A document wider than the stage
 *               (the width bridge in utils/iframe-width-bridge.ts reports it)
 *               is laid out at its own width and scaled down to fit, so
 *               nothing hides past the right edge — when that width holds
 *               it; a document that only grows with its viewport keeps
 *               filling the stage. The default.
 *   - desktop / tablet / phone : the iframe is exactly 1440 / 768 / 390 CSS px
 *               wide, so the document's media queries see that device. When
 *               the stage is narrower the frame scales down to fit — layout
 *               stays at the device width, only the pixels shrink — and the
 *               caption says by how much. Height always fills the stage.
 *
 * The iframe itself is the caller's: the viewer and the full-page preview
 * wire different ones, and this only owns the box around it. Fit only
 * scales when the caller's document carries the width bridge.
 */

import { useEffect, useLayoutEffect, useState, type ReactNode } from "react";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import {
  HTML_PREVIEW_WIDTH_KEY,
  readHtmlPreviewWidthMessage,
  type HtmlPreviewWidthMessage,
} from "./utils/iframe-width-bridge";

export type HtmlViewport = "fill" | "desktop" | "tablet" | "phone";

export const HTML_VIEWPORTS: readonly HtmlViewport[] = [
  "fill",
  "desktop",
  "tablet",
  "phone",
];

export const VIEWPORT_WIDTHS: Record<Exclude<HtmlViewport, "fill">, number> = {
  desktop: 1440,
  tablet: 768,
  phone: 390,
};

/** The widest layout Fit will scale down from; past it the document fills the stage and scrolls. */
export const FIT_MAX_WIDTH = VIEWPORT_WIDTHS.desktop * 2;

export interface ViewportFit {
  /** CSS px the document lays out in. */
  width: number;
  height: number;
  /** ≤ 1 — never scaled up past the device's own size. */
  scale: number;
}

/** Fits a device `width` into `area`: scale by width, fill the height. */
export function fitViewport(
  area: { width: number; height: number },
  width: number,
): ViewportFit {
  const scale = Math.min(1, area.width / width);
  return { width, height: Math.round(area.height / scale), scale };
}

// The untransformed layout box (offsetWidth / offsetHeight), not a client
// rect: the viewer's stage scales in on open, and a rect measured mid-
// animation would fit against a box a few percent too small.
function useLayoutSize(node: HTMLElement | null) {
  const [size, setSize] = useState<{ width: number; height: number } | null>(null);
  useLayoutEffect(() => {
    if (!node) return;
    const measure = () => {
      const width = node.offsetWidth;
      const height = node.offsetHeight;
      setSize((previous) =>
        previous?.width === width && previous.height === height
          ? previous
          : { width, height },
      );
    };
    measure();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(measure);
    observer.observe(node);
    return () => observer.disconnect();
  }, [node]);
  return size;
}

/**
 * Fit's one try at widening a document, for one document at one stage width.
 * `failed` once the document still overflowed at `width`: its overflow grows
 * with the viewport (a `width: 100%` box with padding, a breakpoint that
 * jumps wider), so widening cannot settle it and Fit fills the stage instead.
 */
export interface FitAttempt {
  doc: string;
  stage: number;
  width: number;
  failed: boolean;
}

// Widths are whole CSS px on both sides, but the stage's offsetWidth and the
// frame's innerWidth may round a fractional box differently.
const near = (a: number, b: number) => Math.abs(a - b) <= 1;

/**
 * The attempt after a width report. Only a report measured at the stage's
 * own width starts an attempt; only one measured at the attempt's width
 * settles it. Anything else — a device width's layout, a report in flight
 * while the frame resizes — is not about the layout Fit is deciding on.
 */
export function nextFitAttempt(
  previous: FitAttempt | null,
  report: HtmlPreviewWidthMessage,
  stage: number,
): FitAttempt | null {
  const current =
    previous?.doc === report.doc && previous.stage === stage ? previous : null;
  if (!current) {
    if (!near(report.viewport, stage)) return null;
    if (report.needed <= report.viewport || report.needed > FIT_MAX_WIDTH) return null;
    return { doc: report.doc, stage, width: report.needed, failed: false };
  }
  if (current.failed || !near(report.viewport, current.width)) return current;
  return report.needed > report.viewport ? { ...current, failed: true } : current;
}

// The width Fit lays the document out at when that is wider than the stage,
// from the width bridge's reports; null fills the stage. Leaving Fit forgets
// the attempt, so coming back measures afresh against the stage.
function useFitWidth(area: HTMLElement | null, active: boolean, stage: number | null) {
  const [attempt, setAttempt] = useState<FitAttempt | null>(null);
  const [wasActive, setWasActive] = useState(active);
  if (wasActive !== active) {
    setWasActive(active);
    setAttempt(null);
  }

  useEffect(() => {
    if (!area || !active || stage === null) return;
    const frame = () => area.querySelector("iframe")?.contentWindow ?? null;
    const onMessage = (event: MessageEvent) => {
      if (!event.source || event.source !== frame()) return;
      const report = readHtmlPreviewWidthMessage(event.data);
      if (report) setAttempt((previous) => nextFitAttempt(previous, report, stage));
    };
    window.addEventListener("message", onMessage);
    // The frame may already be at the stage's width (back from a device
    // width the same size), and then no resize inside it reports.
    frame()?.postMessage({ [HTML_PREVIEW_WIDTH_KEY]: 1, type: "measure" }, "*");
    return () => window.removeEventListener("message", onMessage);
  }, [area, active, stage]);

  return active &&
    attempt &&
    !attempt.failed &&
    attempt.stage === stage &&
    attempt.width > attempt.stage
    ? attempt.width
    : null;
}

export function HtmlViewportFrame({
  viewport,
  children,
}: {
  viewport: HtmlViewport;
  /** The iframe, sized `h-full w-full`. */
  children: ReactNode;
}) {
  const { t } = useT("editor");
  const [area, setArea] = useState<HTMLDivElement | null>(null);
  const size = useLayoutSize(area);
  const fitWidth = useFitWidth(area, viewport === "fill", size?.width || null);

  // The width the document lays out at; null fills the stage.
  const layoutWidth = viewport === "fill" ? fitWidth : VIEWPORT_WIDTHS[viewport];
  // Until the area has a size (first layout, or a host without layout such
  // as a test DOM) a device frame shows unscaled at full height.
  const fit =
    layoutWidth !== null && size && size.width > 0
      ? fitViewport(size, layoutWidth)
      : null;
  const percent = fit ? Math.round(fit.scale * 100) : 100;

  // One element tree for every viewport, so switching only resizes the
  // iframe — it never remounts, and the document keeps its state.
  return (
    <div className={cn("flex h-full flex-col", layoutWidth === null ? "pb-4" : "pb-2")}>
      <div ref={setArea} className="relative min-h-0 flex-1">
        {/* The visible box is the scaled size; inside it the frame lays out
            at its full layout width and a transform shrinks it to fit. */}
        <div
          className="mx-auto h-full max-w-full overflow-hidden rounded-lg"
          style={{
            width:
              layoutWidth === null
                ? "100%"
                : fit
                  ? layoutWidth * fit.scale
                  : layoutWidth,
          }}
          data-testid="html-viewport"
        >
          <div
            className="h-full w-full"
            style={
              layoutWidth === null
                ? undefined
                : fit
                  ? {
                      width: fit.width,
                      height: fit.height,
                      transform: fit.scale < 1 ? `scale(${fit.scale})` : undefined,
                      transformOrigin: "top left",
                    }
                  : { width: layoutWidth }
            }
          >
            {children}
          </div>
        </div>
      </div>
      {layoutWidth !== null && (
        <p className="dark shrink-0 pt-2 text-center text-caption tabular-nums text-muted-foreground">
          {fit ? `${fit.width} × ${fit.height}` : layoutWidth}
          {percent < 100 && (
            <>
              {" · "}
              {t(($) => $.attachment.viewport_scale, { percent })}
            </>
          )}
        </p>
      )}
    </div>
  );
}
