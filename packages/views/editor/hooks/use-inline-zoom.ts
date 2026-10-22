"use client";

/**
 * Zoom for the inline Mermaid preview (MUL-7766).
 *
 * The preview opens fitted — the whole diagram inside the column's width and
 * at most `maxHeight` tall — so a reader browsing a thread sees every diagram
 * in full without opening anything. The zoom buttons step from there, and the
 * preview box scrolls natively once the diagram outgrows it.
 *
 * Deliberately not `useZoomCanvas`. That hook owns the wheel and every touch
 * gesture, which is right for a full-screen viewer and wrong inside a
 * document, where those belong to the page. It also zooms with a transform;
 * here the diagram is an SVG that fills its iframe, so zooming is just a
 * bigger iframe, and every step is redrawn as vectors instead of scaling a
 * bitmap.
 */

import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type RefObject,
} from "react";
import {
  MAX_SCALE,
  ZOOM_STEP,
  computeFitScale,
  type Point,
  type Size,
} from "../utils/zoom-transform";

export interface InlineZoom {
  scale: number;
  /** Drawn size of the diagram at `scale`, in whole pixels. */
  size: Size;
  /** True while the preview shows the whole diagram, following the column's width. */
  fitted: boolean;
  /** Whole-percent zoom for the readout. */
  zoomPercent: number;
  canZoomIn: boolean;
  canZoomOut: boolean;
  zoomIn: () => void;
  zoomOut: () => void;
  fit: () => void;
}

/**
 * One zoom step from `scale`, landing on 100% on the way past it: natural size
 * is the one stop worth hitting exactly, and 1.2 steps from an arbitrary fit
 * scale would otherwise skip over it.
 */
export function stepScale(scale: number, factor: number): number {
  const next = scale * factor;
  if ((scale < 1 && next > 1) || (scale > 1 && next < 1)) return 1;
  return next;
}

export function useInlineZoom({
  content,
  maxHeight,
  scrollRef,
}: {
  /** Natural size of the diagram, or null before it has rendered. */
  content: Size | null;
  /** Tallest the fitted diagram may be drawn. */
  maxHeight: number;
  /** The preview's scroll box: measured for the fit, scrolled to keep zoom centered. */
  scrollRef: RefObject<HTMLElement | null>;
}): InlineZoom {
  const [availableWidth, setAvailableWidth] = useState(0);
  // null = fitted. Stored apart from the scale so a fitted preview keeps
  // fitting as the column resizes, instead of freezing at the old width's fit.
  const [zoom, setZoom] = useState<number | null>(null);
  const hasContent = content !== null;

  // Layout effect: the first painted frame must already be fitted, not a
  // natural-size diagram that then snaps down.
  useLayoutEffect(() => {
    const element = scrollRef.current;
    if (!element) return;

    // offsetWidth, not clientWidth: it includes a vertical scrollbar, so the
    // fit does not change when zooming in makes one appear. Zero means hidden
    // (a frame showing its source view), not a zero-wide column; keep the
    // last real width rather than refit against nothing.
    const measure = () => {
      const width = element.offsetWidth;
      if (width > 0) setAvailableWidth(width);
    };
    measure();

    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, [scrollRef, hasContent]);

  // A different diagram starts fitted again; a re-render of the same one (a
  // theme switch) keeps the reader's zoom.
  useEffect(() => {
    setZoom(null);
  }, [content?.width, content?.height]);

  const natural = content ?? { width: 1, height: 1 };
  const fitScale = computeFitScale(natural, {
    // Unmeasured (no layout, e.g. jsdom) constrains the height only.
    width: availableWidth > 0 ? availableWidth : natural.width,
    height: maxHeight,
  });
  const scale = zoom === null ? fitScale : Math.max(fitScale, zoom);
  const fitted = scale <= fitScale + 0.001;
  const size: Size = {
    width: Math.max(1, Math.floor(natural.width * scale)),
    height: Math.max(1, Math.floor(natural.height * scale)),
  };

  // Latest-value ref so the button callbacks stay stable.
  const stateRef = useRef({ scale, fitScale, size });
  stateRef.current = { scale, fitScale, size };
  // Content point at the center of the box, carried across a zoom step.
  const anchorRef = useRef<Point | null>(null);

  const zoomTo = useCallback(
    (next: number | null) => {
      const element = scrollRef.current;
      const { scale: current, fitScale: fitAt, size: currentSize } = stateRef.current;
      const target = next === null ? fitAt : Math.max(fitAt, next);
      // Only a real step carries an anchor: one left behind by a no-op would
      // fire on the next resize and scroll the box on its own.
      if (element && Math.abs(target - current) > 0.0005) {
        // A diagram narrower than the box is centered by auto margins.
        const offsetX = Math.max(0, (element.clientWidth - currentSize.width) / 2);
        anchorRef.current = {
          x: (element.scrollLeft + element.clientWidth / 2 - offsetX) / current,
          y: (element.scrollTop + element.clientHeight / 2) / current,
        };
      }
      setZoom(next);
    },
    [scrollRef],
  );

  // Buttons zoom about the middle of what is on screen, so a step never
  // throws the reader back to the diagram's top-left corner.
  useLayoutEffect(() => {
    const anchor = anchorRef.current;
    anchorRef.current = null;
    const element = scrollRef.current;
    if (!anchor || !element) return;

    const offsetX = Math.max(0, (element.clientWidth - size.width) / 2);
    element.scrollLeft = anchor.x * scale + offsetX - element.clientWidth / 2;
    element.scrollTop = anchor.y * scale - element.clientHeight / 2;
  }, [scrollRef, scale, size.width]);

  const zoomIn = useCallback(() => {
    const { scale: current } = stateRef.current;
    zoomTo(Math.min(MAX_SCALE, stepScale(current, ZOOM_STEP)));
  }, [zoomTo]);

  const zoomOut = useCallback(() => {
    const { scale: current, fitScale: fitAt } = stateRef.current;
    const next = stepScale(current, 1 / ZOOM_STEP);
    // Nothing below the fit: it already shows the whole diagram.
    zoomTo(next <= fitAt + 0.001 ? null : next);
  }, [zoomTo]);

  const fit = useCallback(() => zoomTo(null), [zoomTo]);

  return {
    scale,
    size,
    fitted,
    zoomPercent: Math.round(scale * 100),
    canZoomIn: scale < MAX_SCALE - 0.001,
    canZoomOut: !fitted,
    zoomIn,
    zoomOut,
    fit,
  };
}
