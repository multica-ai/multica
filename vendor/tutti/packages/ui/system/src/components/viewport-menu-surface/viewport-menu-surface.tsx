import * as React from "react";
import { createPortal } from "react-dom";

import { MenuSurface } from "../menu-surface";
import { getPortalContainer } from "../../lib/portal-container";

export interface MenuSize {
  width: number;
  height: number;
}

export interface MenuPoint {
  x: number;
  y: number;
}

export type MenuPointAlignment = "start" | "end" | "auto";

interface AbsoluteViewportMenuPlacement {
  type: "absolute";
  left: number;
  top: number;
  boundaryPoint?: MenuPoint;
  constrainToBoundary?: boolean;
}

interface PointViewportMenuPlacement {
  type: "point";
  point: MenuPoint;
  boundaryPoint?: MenuPoint;
  alignX?: MenuPointAlignment;
  alignY?: MenuPointAlignment;
  padding?: number;
  estimatedSize?: MenuSize;
}

export type ViewportMenuPlacement =
  | AbsoluteViewportMenuPlacement
  | PointViewportMenuPlacement;

export interface ViewportMenuSurfaceProps extends Omit<
  React.HTMLAttributes<HTMLDivElement>,
  "children"
> {
  open: boolean;
  placement: ViewportMenuPlacement;
  children: React.ReactNode;
  onDismiss?: () => void;
  dismissOnPointerDownOutside?: boolean;
  dismissOnEscape?: boolean;
  dismissOnScroll?: boolean;
  dismissIgnoreRefs?: Array<React.RefObject<HTMLElement | null>>;
  stopEventPropagation?: boolean;
}

const VIEWPORT_MENU_PADDING = 12;
const MENU_BOUNDARY_PADDING = 8;

interface MenuBoundaryRect {
  left: number;
  top: number;
  width: number;
  height: number;
}

interface MenuBoundary {
  portalTarget: Element | null;
  rect: MenuBoundaryRect;
}

function clampMenuCoordinate(
  origin: number,
  size: number,
  viewportExtent: number,
  padding: number
): number {
  return Math.max(
    padding,
    Math.min(origin, Math.max(padding, viewportExtent - padding - size))
  );
}

function resolveAlignedCoordinate(options: {
  origin: number;
  size: number;
  viewportExtent: number;
  padding: number;
  alignment: MenuPointAlignment;
}): number {
  const { origin, size, viewportExtent, padding, alignment } = options;
  const startCoordinate = origin;
  const endCoordinate = origin - size;

  if (alignment === "start") {
    return clampMenuCoordinate(startCoordinate, size, viewportExtent, padding);
  }

  if (alignment === "end") {
    return clampMenuCoordinate(endCoordinate, size, viewportExtent, padding);
  }

  const startFits = startCoordinate + size <= viewportExtent - padding;
  const endFits = endCoordinate >= padding;

  if (startFits || !endFits) {
    return clampMenuCoordinate(startCoordinate, size, viewportExtent, padding);
  }

  return clampMenuCoordinate(endCoordinate, size, viewportExtent, padding);
}

function placeViewportMenuAtPoint(options: {
  point: MenuPoint;
  menuSize: MenuSize;
  viewport: { width: number; height: number };
  padding?: number;
  alignX?: MenuPointAlignment;
  alignY?: MenuPointAlignment;
}): { left: number; top: number } {
  const padding = options.padding ?? VIEWPORT_MENU_PADDING;

  return {
    left: resolveAlignedCoordinate({
      origin: options.point.x,
      size: options.menuSize.width,
      viewportExtent: options.viewport.width,
      padding,
      alignment: options.alignX ?? "start"
    }),
    top: resolveAlignedCoordinate({
      origin: options.point.y,
      size: options.menuSize.height,
      viewportExtent: options.viewport.height,
      padding,
      alignment: options.alignY ?? "start"
    })
  };
}

function viewportBoundary(): MenuBoundaryRect {
  return {
    left: 0,
    top: 0,
    width: typeof window === "undefined" ? 1280 : window.innerWidth,
    height: typeof window === "undefined" ? 720 : window.innerHeight
  };
}

function rectToBoundary(rect: DOMRect): MenuBoundaryRect {
  return {
    left: rect.left,
    top: rect.top,
    width: rect.width,
    height: rect.height
  };
}

function resolveMenuBoundaryFromPoint(point: {
  x: number;
  y: number;
}): MenuBoundary {
  if (typeof document === "undefined" || !document.elementsFromPoint) {
    return {
      portalTarget: null,
      rect: viewportBoundary()
    };
  }

  const selector = '[data-slot="viewport-menu-boundary"]';
  for (const element of document.elementsFromPoint(point.x, point.y)) {
    const boundaryElement = element.closest(selector);
    if (boundaryElement) {
      return {
        portalTarget:
          boundaryElement.getAttribute("data-viewport-menu-portal-target") ===
          "body"
            ? null
            : boundaryElement,
        rect: rectToBoundary(boundaryElement.getBoundingClientRect())
      };
    }
  }

  return {
    portalTarget: null,
    rect: viewportBoundary()
  };
}

function clampMenuPositionToBoundary(options: {
  left: number;
  top: number;
  width: number;
  height: number;
  boundary: MenuBoundaryRect;
  padding?: number;
}): { left: number; top: number } {
  const padding = options.padding ?? MENU_BOUNDARY_PADDING;
  const minLeft = options.boundary.left + padding;
  const minTop = options.boundary.top + padding;
  const maxLeft = Math.max(
    minLeft,
    options.boundary.left + options.boundary.width - padding - options.width
  );
  const maxTop = Math.max(
    minTop,
    options.boundary.top + options.boundary.height - padding - options.height
  );

  return {
    left: Math.max(minLeft, Math.min(options.left, maxLeft)),
    top: Math.max(minTop, Math.min(options.top, maxTop))
  };
}

function assignRef<T>(ref: React.ForwardedRef<T>, value: T): void {
  if (typeof ref === "function") {
    ref(value);
    return;
  }

  if (ref) {
    ref.current = value;
  }
}

function callHandler<E extends React.SyntheticEvent>(
  handler: ((event: E) => void) | undefined,
  event: E
): void {
  handler?.(event);
}

const ViewportMenuSurface = React.forwardRef<
  HTMLDivElement,
  ViewportMenuSurfaceProps
>(function ViewportMenuSurface(
  {
    open,
    placement,
    children,
    onDismiss,
    dismissOnPointerDownOutside = false,
    dismissOnEscape = false,
    dismissOnScroll = false,
    dismissIgnoreRefs = [],
    stopEventPropagation = true,
    style,
    onMouseDown,
    onClick,
    className,
    ...rest
  },
  forwardedRef
): React.JSX.Element | null {
  const surfaceRef = React.useRef<HTMLDivElement | null>(null);
  const [measuredSize, setMeasuredSize] = React.useState<MenuSize | null>(null);

  const setRefs = React.useCallback(
    (node: HTMLDivElement | null) => {
      surfaceRef.current = node;
      assignRef(forwardedRef, node);
    },
    [forwardedRef]
  );

  React.useLayoutEffect(() => {
    if (!open) {
      setMeasuredSize(null);
      return;
    }

    const element = surfaceRef.current;
    if (!element) {
      setMeasuredSize(null);
      return;
    }

    const updateMeasuredSize = () => {
      const rect = element.getBoundingClientRect();
      setMeasuredSize((previous) =>
        previous &&
        Math.abs(previous.width - rect.width) < 0.5 &&
        Math.abs(previous.height - rect.height) < 0.5
          ? previous
          : { width: rect.width, height: rect.height }
      );
    };

    updateMeasuredSize();

    if (typeof ResizeObserver === "undefined") {
      return;
    }

    const observer = new ResizeObserver(updateMeasuredSize);
    observer.observe(element);
    return () => observer.disconnect();
  }, [open, placement]);

  React.useEffect(() => {
    if (!open) {
      return;
    }

    if (
      !onDismiss ||
      (!dismissOnPointerDownOutside && !dismissOnEscape && !dismissOnScroll)
    ) {
      return;
    }

    const shouldIgnoreTarget = (target: EventTarget | null): boolean => {
      if (!(target instanceof Node)) {
        return false;
      }

      if (surfaceRef.current?.contains(target)) {
        return true;
      }

      return dismissIgnoreRefs.some(
        (ref) => ref.current?.contains(target) ?? false
      );
    };

    const handlePointerDown = (event: PointerEvent) => {
      if (!dismissOnPointerDownOutside) {
        return;
      }
      if (shouldIgnoreTarget(event.target)) {
        return;
      }
      onDismiss();
    };

    const handleKeyDown = (event: KeyboardEvent) => {
      if (!dismissOnEscape || event.key !== "Escape") {
        return;
      }
      onDismiss();
    };

    const handleScroll = () => {
      if (dismissOnScroll) {
        onDismiss();
      }
    };

    document.addEventListener("pointerdown", handlePointerDown, true);
    window.addEventListener("keydown", handleKeyDown, true);
    window.addEventListener("scroll", handleScroll, {
      capture: true,
      passive: true
    });

    return () => {
      document.removeEventListener("pointerdown", handlePointerDown, true);
      window.removeEventListener("keydown", handleKeyDown, true);
      window.removeEventListener("scroll", handleScroll, true);
    };
  }, [
    dismissIgnoreRefs,
    dismissOnEscape,
    dismissOnPointerDownOutside,
    dismissOnScroll,
    onDismiss,
    open
  ]);

  const resolvedPlacement = React.useMemo(() => {
    if (placement.type === "absolute") {
      const boundary = resolveMenuBoundaryFromPoint(
        placement.boundaryPoint ?? {
          x: placement.left,
          y: placement.top
        }
      );
      const menuSize = measuredSize ?? { width: 0, height: 0 };
      return {
        portalTarget: boundary.portalTarget,
        position:
          placement.constrainToBoundary === false
            ? {
                left: placement.left,
                top: placement.top
              }
            : clampMenuPositionToBoundary({
                left: placement.left,
                top: placement.top,
                width: menuSize.width,
                height: menuSize.height,
                boundary: boundary.rect
              })
      };
    }

    const boundary = resolveMenuBoundaryFromPoint(
      placement.boundaryPoint ?? placement.point
    );
    const menuSize = measuredSize ??
      placement.estimatedSize ?? { width: 0, height: 0 };
    const relativePoint = {
      x: placement.point.x - boundary.rect.left,
      y: placement.point.y - boundary.rect.top
    };
    const relativePosition = placeViewportMenuAtPoint({
      point: relativePoint,
      menuSize,
      viewport: { width: boundary.rect.width, height: boundary.rect.height },
      padding: placement.padding,
      alignX: placement.alignX,
      alignY: placement.alignY
    });

    return {
      portalTarget: boundary.portalTarget,
      position: {
        left: boundary.rect.left + relativePosition.left,
        top: boundary.rect.top + relativePosition.top
      }
    };
  }, [measuredSize, placement]);

  if (!open || typeof document === "undefined" || !document.body) {
    return null;
  }

  const portalTarget = resolvedPlacement.portalTarget ?? getPortalContainer();

  return createPortal(
    <MenuSurface
      {...rest}
      ref={setRefs}
      className={className}
      data-slot="viewport-menu-surface"
      style={{
        position: "fixed",
        top: resolvedPlacement.position.top,
        left: resolvedPlacement.position.left,
        zIndex: "var(--z-popover)",
        ...style
      }}
      onClick={(event) => {
        if (stopEventPropagation) {
          event.stopPropagation();
        }
        callHandler(onClick, event);
      }}
      onMouseDown={(event) => {
        if (stopEventPropagation) {
          event.stopPropagation();
        }
        callHandler(onMouseDown, event);
      }}
    >
      {children}
    </MenuSurface>,
    portalTarget
  );
});

export { ViewportMenuSurface };
