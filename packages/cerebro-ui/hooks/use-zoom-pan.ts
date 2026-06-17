"use client";

import { useCallback, useEffect, useRef, useState } from "react";

// ---------------------------------------------------------------------------
// useZoomPan — pinch / wheel zoom + drag-pan for a non-responsive preview.
// ---------------------------------------------------------------------------
//
// Powers the image/media previews that previously had no zoom at all:
//   - Desktop: wheel up/down zooms toward the cursor; click-drag pans when
//     zoomed in.
//   - Mobile:  two-finger pinch zooms toward the gesture midpoint; one-finger
//     drag pans when zoomed in.
//   - Double-click / double-tap toggles between fit (1x) and a 2.5x focus.
//
// The hook owns a `viewport` ref (the clipping box) and a `content` ref (the
// element that gets transformed). It attaches non-passive `wheel` / `touchmove`
// listeners so it can call preventDefault — without that the browser would
// scroll the page or trigger native page-zoom instead of zooming the preview.
//
// State is intentionally local (no Zustand): a preview zoom is ephemeral UI
// state scoped to one open modal, exactly the case the store rules exclude.

export interface ZoomPanOptions {
  /** Largest allowed scale factor. Default 8. */
  maxScale?: number;
  /** Smallest allowed scale factor. Default 1 (fit). */
  minScale?: number;
  /** Wheel sensitivity; higher = faster zoom. Default 0.0015. */
  wheelStep?: number;
  /** Scale applied on a double-click / double-tap from fit. Default 2.5. */
  doubleTapScale?: number;
}

export interface ZoomPanState {
  /** Ref for the clipping viewport (overflow-hidden box). All gestures bind
   *  here; the transformed child just receives `transform`. */
  viewportRef: React.RefObject<HTMLDivElement | null>;
  /** Current scale factor (>= minScale). */
  scale: number;
  /** Inline transform to apply to the content element. */
  transform: string;
  /** True once the user has zoomed past fit — callers use it to switch the
   *  cursor and to stop a click from closing a lightbox after a pan. */
  isZoomed: boolean;
  /** Reset back to fit (1x, centered). */
  reset: () => void;
}

interface Point {
  x: number;
  y: number;
}

const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v));

/**
 * Pure anchored-zoom math: given the current scale + pan offset and a target
 * scale, return the clamped scale and the offset that keeps the pixel under
 * `anchor` (a viewport-local point) stationary. The content is centered in the
 * viewport (transform-origin: center), so anchors are measured against the
 * viewport center. Snapping back to `minScale` recenters (offset 0,0).
 *
 * Exported for unit testing — the hook is otherwise DOM/event driven.
 */
export function anchoredZoom(params: {
  prevScale: number;
  nextScale: number;
  anchor: Point;
  viewport: { width: number; height: number };
  offset: Point;
  minScale: number;
  maxScale: number;
}): { scale: number; offset: Point } {
  const { prevScale, anchor, viewport, offset, minScale, maxScale } = params;
  const next = clamp(params.nextScale, minScale, maxScale);
  if (next === prevScale) return { scale: prevScale, offset };
  if (next === minScale) return { scale: minScale, offset: { x: 0, y: 0 } };

  const cx = viewport.width / 2;
  const cy = viewport.height / 2;
  const ax = anchor.x - cx - offset.x;
  const ay = anchor.y - cy - offset.y;
  const ratio = next / prevScale;
  return {
    scale: next,
    offset: {
      x: offset.x - ax * (ratio - 1),
      y: offset.y - ay * (ratio - 1),
    },
  };
}

export function useZoomPan(options: ZoomPanOptions = {}): ZoomPanState {
  const {
    maxScale = 8,
    minScale = 1,
    wheelStep = 0.0015,
    doubleTapScale = 2.5,
  } = options;

  const viewportRef = useRef<HTMLDivElement | null>(null);

  const [scale, setScale] = useState(minScale);
  const [offset, setOffset] = useState<Point>({ x: 0, y: 0 });

  // Mutable gesture bookkeeping kept in refs so listeners don't re-bind on
  // every render and don't fight React's batching mid-gesture.
  const scaleRef = useRef(scale);
  const offsetRef = useRef(offset);
  scaleRef.current = scale;
  offsetRef.current = offset;

  const pinchStart = useRef<{ dist: number; scale: number; mid: Point } | null>(null);
  const panStart = useRef<{ pointer: Point; offset: Point } | null>(null);

  const reset = useCallback(() => {
    setScale(minScale);
    setOffset({ x: 0, y: 0 });
  }, [minScale]);

  // Apply a scale change anchored at a viewport-local point (cursor / pinch
  // midpoint) so the pixel under the gesture stays put while zooming. The
  // content is centered in the viewport (transform-origin: center), so anchor
  // coordinates are measured relative to the viewport center.
  const zoomAt = useCallback(
    (nextScaleRaw: number, anchor: Point) => {
      const vp = viewportRef.current;
      if (!vp) return;
      const rect = vp.getBoundingClientRect();
      const result = anchoredZoom({
        prevScale: scaleRef.current,
        nextScale: nextScaleRaw,
        anchor,
        viewport: { width: rect.width, height: rect.height },
        offset: offsetRef.current,
        minScale,
        maxScale,
      });
      setScale(result.scale);
      setOffset(result.offset);
    },
    [maxScale, minScale],
  );

  const localPoint = useCallback((clientX: number, clientY: number): Point => {
    const vp = viewportRef.current;
    if (!vp) return { x: 0, y: 0 };
    const rect = vp.getBoundingClientRect();
    return { x: clientX - rect.left, y: clientY - rect.top };
  }, []);

  // All gesture listeners live on the viewport. Bound once; they read live
  // scale/offset from refs so the effect never needs to re-run on each frame.
  useEffect(() => {
    const vp = viewportRef.current;
    if (!vp) return;

    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      const factor = Math.exp(-e.deltaY * wheelStep);
      zoomAt(scaleRef.current * factor, localPoint(e.clientX, e.clientY));
    };

    const onMouseDown = (e: MouseEvent) => {
      if (scaleRef.current <= minScale) return;
      e.preventDefault();
      const startOffset = offsetRef.current;
      const startX = e.clientX;
      const startY = e.clientY;
      const move = (ev: MouseEvent) => {
        setOffset({
          x: startOffset.x + (ev.clientX - startX),
          y: startOffset.y + (ev.clientY - startY),
        });
      };
      const up = () => {
        window.removeEventListener("mousemove", move);
        window.removeEventListener("mouseup", up);
      };
      window.addEventListener("mousemove", move);
      window.addEventListener("mouseup", up);
    };

    const onDblClick = (e: MouseEvent) => {
      e.preventDefault();
      if (scaleRef.current > minScale) {
        reset();
      } else {
        zoomAt(doubleTapScale, localPoint(e.clientX, e.clientY));
      }
    };

    const onTouchStart = (e: TouchEvent) => {
      const t0 = e.touches[0];
      const t1 = e.touches[1];
      if (t0 && t1) {
        const a = localPoint(t0.clientX, t0.clientY);
        const b = localPoint(t1.clientX, t1.clientY);
        pinchStart.current = {
          dist: Math.hypot(a.x - b.x, a.y - b.y),
          scale: scaleRef.current,
          mid: { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 },
        };
        panStart.current = null;
      } else if (t0 && e.touches.length === 1 && scaleRef.current > minScale) {
        panStart.current = {
          pointer: localPoint(t0.clientX, t0.clientY),
          offset: offsetRef.current,
        };
        pinchStart.current = null;
      }
    };

    const onTouchMove = (e: TouchEvent) => {
      const t0 = e.touches[0];
      const t1 = e.touches[1];
      if (t0 && t1 && pinchStart.current) {
        e.preventDefault();
        const a = localPoint(t0.clientX, t0.clientY);
        const b = localPoint(t1.clientX, t1.clientY);
        const dist = Math.hypot(a.x - b.x, a.y - b.y);
        const ratio = dist / (pinchStart.current.dist || 1);
        zoomAt(pinchStart.current.scale * ratio, pinchStart.current.mid);
      } else if (
        t0 &&
        e.touches.length === 1 &&
        panStart.current &&
        scaleRef.current > minScale
      ) {
        e.preventDefault();
        const p = localPoint(t0.clientX, t0.clientY);
        setOffset({
          x: panStart.current.offset.x + (p.x - panStart.current.pointer.x),
          y: panStart.current.offset.y + (p.y - panStart.current.pointer.y),
        });
      }
    };

    const onTouchEnd = (e: TouchEvent) => {
      if (e.touches.length < 2) pinchStart.current = null;
      if (e.touches.length === 0) panStart.current = null;
    };

    vp.addEventListener("wheel", onWheel, { passive: false });
    vp.addEventListener("mousedown", onMouseDown);
    vp.addEventListener("dblclick", onDblClick);
    vp.addEventListener("touchstart", onTouchStart, { passive: false });
    vp.addEventListener("touchmove", onTouchMove, { passive: false });
    vp.addEventListener("touchend", onTouchEnd);
    vp.addEventListener("touchcancel", onTouchEnd);
    return () => {
      vp.removeEventListener("wheel", onWheel);
      vp.removeEventListener("mousedown", onMouseDown);
      vp.removeEventListener("dblclick", onDblClick);
      vp.removeEventListener("touchstart", onTouchStart);
      vp.removeEventListener("touchmove", onTouchMove);
      vp.removeEventListener("touchend", onTouchEnd);
      vp.removeEventListener("touchcancel", onTouchEnd);
    };
  }, [doubleTapScale, localPoint, minScale, reset, wheelStep, zoomAt]);

  const transform = `translate(${offset.x}px, ${offset.y}px) scale(${scale})`;

  return {
    viewportRef,
    scale,
    transform,
    isZoomed: scale > minScale,
    reset,
  };
}
