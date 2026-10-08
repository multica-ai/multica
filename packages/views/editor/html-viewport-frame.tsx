"use client";

/**
 * HtmlViewportFrame — lays an HTML attachment's iframe out at a chosen
 * viewport width, for the viewer's html kind.
 *
 *   - fill    : the iframe takes the whole stage, as the page would in a
 *               browser window that size. The default.
 *   - desktop / tablet / phone : the iframe is exactly 1440 / 768 / 390 CSS px
 *               wide, so the document's media queries see that device. When
 *               the stage is narrower the frame scales down to fit — layout
 *               stays at the device width, only the pixels shrink — and the
 *               caption says by how much. Height always fills the stage.
 *
 * The iframe itself is the caller's: the viewer and the full-page preview
 * wire different ones, and this only owns the box around it.
 */

import { useLayoutEffect, useState, type ReactNode } from "react";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";

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

  const deviceWidth = viewport === "fill" ? null : VIEWPORT_WIDTHS[viewport];
  // Until the area has a size (first layout, or a host without layout such
  // as a test DOM) a device frame shows unscaled at full height.
  const fit =
    deviceWidth !== null && size && size.width > 0
      ? fitViewport(size, deviceWidth)
      : null;
  const percent = fit ? Math.round(fit.scale * 100) : 100;

  // One element tree for every viewport, so switching only resizes the
  // iframe — it never remounts, and the document keeps its state.
  return (
    <div className={cn("flex h-full flex-col", deviceWidth === null ? "pb-4" : "pb-2")}>
      <div ref={setArea} className="relative min-h-0 flex-1">
        {/* The visible box is the scaled size; inside it the frame lays out
            at full device size and a transform shrinks it to fit. */}
        <div
          className="mx-auto h-full max-w-full overflow-hidden rounded-lg"
          style={{
            width:
              deviceWidth === null
                ? "100%"
                : fit
                  ? deviceWidth * fit.scale
                  : deviceWidth,
          }}
          data-testid="html-viewport"
        >
          <div
            className="h-full w-full"
            style={
              deviceWidth === null
                ? undefined
                : fit
                  ? {
                      width: fit.width,
                      height: fit.height,
                      transform: fit.scale < 1 ? `scale(${fit.scale})` : undefined,
                      transformOrigin: "top left",
                    }
                  : { width: deviceWidth }
            }
          >
            {children}
          </div>
        </div>
      </div>
      {deviceWidth !== null && (
        <p className="dark shrink-0 pt-2 text-center text-caption tabular-nums text-muted-foreground">
          {fit ? `${fit.width} × ${fit.height}` : deviceWidth}
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
