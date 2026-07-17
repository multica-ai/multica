"use client";

/**
 * Pan/zoom controller for a diagram rendered inside an empty-sandbox iframe.
 *
 * The iframe cannot script itself, so every gesture is captured on the host
 * viewport element and applied as a CSS transform on the wrapper around it.
 * That inversion is also what keeps Escape working: the iframe is
 * `pointer-events: none`, so it never takes focus, and key events always stay
 * in the host document where the Dialog can hear them.
 *
 * Transform math lives in `../utils/diagram-transform`; this hook owns only
 * event plumbing and state.
 */

import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type PointerEvent as ReactPointerEvent,
  type KeyboardEvent as ReactKeyboardEvent,
} from "react";
import {
  MAX_SCALE,
  MIN_SCALE,
  PAN_STEP_PX,
  ZOOM_STEP,
  centerTransform,
  clampTransform,
  computeFitTransform,
  distanceBetween,
  midpointOf,
  panBy,
  wheelZoomFactor,
  zoomByAtCenter,
  zoomToAt,
  type DiagramTransform,
  type Point,
  type Size,
} from "../utils/diagram-transform";

interface UseDiagramCanvasOptions {
  /** Natural (unscaled) size of the diagram, or null while it is unknown. */
  content: Size | null;
}

export interface DiagramCanvasApi {
  /**
   * Callback ref for the viewport element. Deliberately not a RefObject: the
   * canvas is portaled by the Dialog and attaches in a later commit than this
   * hook's first layout effect, so an effect keyed on `[]` would find no
   * element, skip measuring, and never observe a resize — leaving the canvas
   * permanently unfitted and inert.
   */
  setViewportNode: (node: HTMLDivElement | null) => void;
  transform: DiagramTransform;
  /** Whole-percent zoom for the toolbar readout. */
  zoomPercent: number;
  canZoomIn: boolean;
  canZoomOut: boolean;
  isPanning: boolean;
  /**
   * True when the last change came from a discrete control (button/keyboard)
   * rather than direct manipulation, so the transform can be eased. Dragging
   * and wheel/pinch must track the input 1:1 and are never animated.
   */
  isAnimated: boolean;
  /** True when the view already matches the default fit — lets Reset disable itself. */
  isFitted: boolean;
  zoomIn: () => void;
  zoomOut: () => void;
  zoomToActualSize: () => void;
  fit: () => void;
  reset: () => void;
  handlePointerDown: (event: ReactPointerEvent<HTMLElement>) => void;
  handlePointerMove: (event: ReactPointerEvent<HTMLElement>) => void;
  handlePointerUp: (event: ReactPointerEvent<HTMLElement>) => void;
  handleKeyDown: (event: ReactKeyboardEvent<HTMLElement>) => void;
}

const IDENTITY: DiagramTransform = { scale: 1, x: 0, y: 0 };
const EMPTY_SIZE: Size = { width: 0, height: 0 };

function nearlyEqual(a: number, b: number): boolean {
  return Math.abs(a - b) < 0.5;
}

export function useDiagramCanvas({ content }: UseDiagramCanvasOptions): DiagramCanvasApi {
  const [viewportNode, setViewportNode] = useState<HTMLDivElement | null>(null);
  const [viewport, setViewport] = useState<Size>(EMPTY_SIZE);
  const [transform, setTransform] = useState<DiagramTransform>(IDENTITY);
  const [isPanning, setIsPanning] = useState(false);
  const [isAnimated, setIsAnimated] = useState(false);

  // Fit is deferred until both sizes are known; until then any transform we
  // computed would be against a 0x0 viewport and would have to be thrown away.
  const hasFittedRef = useRef(false);
  const activePointersRef = useRef(new Map<number, Point>());
  const panOriginRef = useRef<{ pointer: Point; transform: DiagramTransform } | null>(null);
  const pinchOriginRef = useRef<{ distance: number; scale: number } | null>(null);

  const contentSize = content ?? EMPTY_SIZE;
  const hasContent = contentSize.width > 0 && contentSize.height > 0;
  const hasViewport = viewport.width > 0 && viewport.height > 0;
  const ready = hasContent && hasViewport;

  // Latest-value refs: the native wheel listener below is registered once with
  // `{ passive: false }` and would otherwise close over stale state.
  const stateRef = useRef({ transform, contentSize, viewport, ready });
  stateRef.current = { transform, contentSize, viewport, ready };

  useLayoutEffect(() => {
    const element = viewportNode;
    if (!element) return;

    const measure = () => {
      const rect = element.getBoundingClientRect();
      setViewport((previous) =>
        nearlyEqual(previous.width, rect.width) && nearlyEqual(previous.height, rect.height)
          ? previous
          : { width: rect.width, height: rect.height },
      );
    };

    measure();

    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(measure);
    observer.observe(element);

    return () => observer.disconnect();
  }, [viewportNode]);

  // Fit once, on first open. Deliberately not re-fitting on later viewport
  // changes: a theme switch re-renders the diagram at the same size, and
  // silently snapping the user's zoom back to fit would lose their place.
  useEffect(() => {
    if (!ready || hasFittedRef.current) return;
    hasFittedRef.current = true;
    setTransform(computeFitTransform(contentSize, viewport));
  }, [ready, contentSize, viewport]);

  // A genuinely different diagram (new natural size) starts fresh.
  const contentKey = `${contentSize.width}x${contentSize.height}`;
  const previousContentKeyRef = useRef(contentKey);
  useEffect(() => {
    if (previousContentKeyRef.current === contentKey) return;
    previousContentKeyRef.current = contentKey;
    if (!ready) return;
    setTransform(computeFitTransform(contentSize, viewport));
  }, [contentKey, ready, contentSize, viewport]);

  // Keep the diagram in view when the window/pane is resized under it.
  useEffect(() => {
    if (!ready) return;
    setTransform((current) => clampTransform(current, contentSize, viewport));
  }, [ready, contentSize, viewport]);

  const fit = useCallback(() => {
    const { contentSize: c, viewport: v, ready: r } = stateRef.current;
    if (!r) return;
    setIsAnimated(true);
    setTransform(computeFitTransform(c, v));
  }, []);

  const zoomToActualSize = useCallback(() => {
    const { contentSize: c, viewport: v, ready: r } = stateRef.current;
    if (!r) return;
    setIsAnimated(true);
    setTransform(clampTransform(centerTransform(c, v, 1), c, v));
  }, []);

  const zoomBy = useCallback((factor: number) => {
    const { transform: t, contentSize: c, viewport: v, ready: r } = stateRef.current;
    if (!r) return;
    setIsAnimated(true);
    setTransform(zoomByAtCenter(t, factor, c, v));
  }, []);

  const zoomIn = useCallback(() => zoomBy(ZOOM_STEP), [zoomBy]);
  const zoomOut = useCallback(() => zoomBy(1 / ZOOM_STEP), [zoomBy]);

  // Wheel must be a native non-passive listener: React routes `onWheel` through
  // a passive root listener, where preventDefault is ignored and the page would
  // scroll behind the dialog instead of the diagram zooming.
  useEffect(() => {
    const element = viewportNode;
    if (!element) return;

    const onWheel = (event: WheelEvent) => {
      const { transform: t, contentSize: c, viewport: v, ready: r } = stateRef.current;
      if (!r) return;
      event.preventDefault();
      setIsAnimated(false);

      const rect = element.getBoundingClientRect();
      const anchor: Point = {
        x: event.clientX - rect.left,
        y: event.clientY - rect.top,
      };
      const factor = wheelZoomFactor(event.deltaY, event.deltaMode);
      setTransform(zoomToAt(t, t.scale * factor, anchor, c, v));
    };

    element.addEventListener("wheel", onWheel, { passive: false });
    return () => element.removeEventListener("wheel", onWheel);
  }, [viewportNode]);

  const localPoint = useCallback(
    (event: ReactPointerEvent<HTMLElement>): Point => {
      const rect = viewportNode?.getBoundingClientRect();
      return {
        x: event.clientX - (rect?.left ?? 0),
        y: event.clientY - (rect?.top ?? 0),
      };
    },
    [viewportNode],
  );

  const handlePointerDown = useCallback(
    (event: ReactPointerEvent<HTMLElement>) => {
      if (!stateRef.current.ready) return;
      // Ignore secondary mouse buttons so right-click never starts a pan.
      if (event.pointerType === "mouse" && event.button !== 0) return;

      setIsAnimated(false);
      const point = localPoint(event);
      activePointersRef.current.set(event.pointerId, point);
      event.currentTarget.setPointerCapture?.(event.pointerId);

      const pointers = [...activePointersRef.current.values()];
      if (pointers.length === 1) {
        panOriginRef.current = { pointer: point, transform: stateRef.current.transform };
        pinchOriginRef.current = null;
        setIsPanning(true);
        return;
      }
      if (pointers.length === 2) {
        // Second finger down: hand off from pan to pinch.
        panOriginRef.current = null;
        pinchOriginRef.current = {
          distance: distanceBetween(pointers[0]!, pointers[1]!),
          scale: stateRef.current.transform.scale,
        };
        setIsPanning(false);
      }
    },
    [localPoint],
  );

  const handlePointerMove = useCallback(
    (event: ReactPointerEvent<HTMLElement>) => {
      if (!activePointersRef.current.has(event.pointerId)) return;
      const { contentSize: c, viewport: v, ready: r } = stateRef.current;
      if (!r) return;

      const point = localPoint(event);
      activePointersRef.current.set(event.pointerId, point);
      const pointers = [...activePointersRef.current.values()];

      if (pointers.length >= 2) {
        const pinchOrigin = pinchOriginRef.current;
        if (!pinchOrigin || pinchOrigin.distance <= 0) return;
        const [first, second] = pointers as [Point, Point];
        const nextScale =
          pinchOrigin.scale * (distanceBetween(first, second) / pinchOrigin.distance);
        setTransform((current) =>
          zoomToAt(current, nextScale, midpointOf(first, second), c, v),
        );
        return;
      }

      const panOrigin = panOriginRef.current;
      if (!panOrigin) return;
      setTransform(
        clampTransform(
          {
            scale: panOrigin.transform.scale,
            x: panOrigin.transform.x + (point.x - panOrigin.pointer.x),
            y: panOrigin.transform.y + (point.y - panOrigin.pointer.y),
          },
          c,
          v,
        ),
      );
    },
    [localPoint],
  );

  const handlePointerUp = useCallback((event: ReactPointerEvent<HTMLElement>) => {
    activePointersRef.current.delete(event.pointerId);
    event.currentTarget.releasePointerCapture?.(event.pointerId);

    const pointers = [...activePointersRef.current.values()];
    if (pointers.length === 1) {
      // Lifting one finger out of a pinch: resume panning from the survivor
      // rather than freezing until the user lifts and re-touches.
      panOriginRef.current = {
        pointer: pointers[0]!,
        transform: stateRef.current.transform,
      };
      pinchOriginRef.current = null;
      setIsPanning(true);
      return;
    }
    if (pointers.length === 0) {
      panOriginRef.current = null;
      pinchOriginRef.current = null;
      setIsPanning(false);
    }
  }, []);

  const handleKeyDown = useCallback(
    (event: ReactKeyboardEvent<HTMLElement>) => {
      const { contentSize: c, viewport: v, ready: r } = stateRef.current;
      if (!r) return;

      // Escape is the Dialog's; never swallow it here.
      switch (event.key) {
        case "+":
        case "=":
          event.preventDefault();
          zoomIn();
          return;
        case "-":
        case "_":
          event.preventDefault();
          zoomOut();
          return;
        case "0":
          event.preventDefault();
          fit();
          return;
        case "ArrowLeft":
        case "ArrowRight":
        case "ArrowUp":
        case "ArrowDown": {
          event.preventDefault();
          setIsAnimated(true);
          const step = event.shiftKey ? PAN_STEP_PX * 3 : PAN_STEP_PX;
          const deltaX =
            event.key === "ArrowLeft" ? step : event.key === "ArrowRight" ? -step : 0;
          const deltaY =
            event.key === "ArrowUp" ? step : event.key === "ArrowDown" ? -step : 0;
          setTransform((current) => panBy(current, deltaX, deltaY, c, v));
          return;
        }
        default:
      }
    },
    [zoomIn, zoomOut, fit],
  );

  const isFitted = useMemo(() => {
    if (!ready) return true;
    const fitted = computeFitTransform(contentSize, viewport);
    return (
      Math.abs(fitted.scale - transform.scale) < 0.001 &&
      nearlyEqual(fitted.x, transform.x) &&
      nearlyEqual(fitted.y, transform.y)
    );
  }, [ready, contentSize, viewport, transform]);

  return {
    setViewportNode,
    transform,
    zoomPercent: Math.round(transform.scale * 100),
    canZoomIn: transform.scale < MAX_SCALE - 0.001,
    canZoomOut: transform.scale > MIN_SCALE + 0.001,
    isPanning,
    isAnimated,
    isFitted,
    zoomIn,
    zoomOut,
    zoomToActualSize,
    fit,
    reset: fit,
    handlePointerDown,
    handlePointerMove,
    handlePointerUp,
    handleKeyDown,
  };
}

export type { DiagramTransform, Size };
export { MIN_SCALE, MAX_SCALE };
