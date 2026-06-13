import { useEffect, useRef } from "react";
import { useTabHistory } from "./use-tab-history";

export const PAGE_NAVIGATION_SURFACE_SELECTOR = "[data-page-navigation-surface]";

type WheelNavigationGesture = "back" | "forward";

interface WheelNavigationOptions {
  minDeltaX: number;
  axisDominanceRatio: number;
  triggerThreshold: number;
  gestureTimeoutMs: number;
  debounceMs: number;
}

const DEFAULT_OPTIONS: WheelNavigationOptions = {
  minDeltaX: 6,
  axisDominanceRatio: 1.35,
  triggerThreshold: 90,
  gestureTimeoutMs: 220,
  debounceMs: 750,
};

const SCROLL_EDGE_EPSILON = 1;

export function wheelGestureFromDeltaX(
  deltaX: number,
): WheelNavigationGesture | null {
  if (deltaX < 0) return "back";
  if (deltaX > 0) return "forward";
  return null;
}

function eventTargetElement(target: EventTarget | null): Element | null {
  if (target instanceof Element) return target;
  if (target instanceof Node) return target.parentElement;
  return null;
}

function isEditableElement(element: Element): boolean {
  if (!(element instanceof HTMLElement)) return false;
  if (element.isContentEditable) return true;
  const tag = element.tagName.toLowerCase();
  return tag === "input" || tag === "textarea" || tag === "select";
}

function isInsideEditableTarget(target: Element, stopAt: Element): boolean {
  for (let current: Element | null = target; current; current = current.parentElement) {
    if (isEditableElement(current)) return true;
    if (current === stopAt) break;
  }
  return false;
}

function canScrollHorizontally(element: Element, deltaX: number): boolean {
  const { clientWidth, scrollLeft, scrollWidth } = element;
  if (scrollWidth <= clientWidth + SCROLL_EDGE_EPSILON) return false;
  if (deltaX < 0) return scrollLeft > SCROLL_EDGE_EPSILON;
  if (deltaX > 0) {
    return scrollLeft + clientWidth < scrollWidth - SCROLL_EDGE_EPSILON;
  }
  return false;
}

function hasScrollableAncestorInDirection(
  target: Element,
  stopAt: Element,
  deltaX: number,
): boolean {
  for (let current: Element | null = target; current; current = current.parentElement) {
    if (canScrollHorizontally(current, deltaX)) return true;
    if (current === stopAt) break;
  }
  return false;
}

export function isPageNavigationWheelCandidate(
  event: WheelEvent,
  options: Pick<WheelNavigationOptions, "minDeltaX" | "axisDominanceRatio"> = DEFAULT_OPTIONS,
): boolean {
  if (event.defaultPrevented) return false;
  if (event.ctrlKey || event.metaKey || event.altKey || event.shiftKey) return false;

  const absX = Math.abs(event.deltaX);
  const absY = Math.abs(event.deltaY);
  if (absX < options.minDeltaX) return false;
  if (absX < absY * options.axisDominanceRatio) return false;

  const target = eventTargetElement(event.target);
  if (!target) return false;

  const surface = target.closest(PAGE_NAVIGATION_SURFACE_SELECTOR);
  if (!surface) return false;

  if (isInsideEditableTarget(target, surface)) return false;
  return !hasScrollableAncestorInDirection(target, surface, event.deltaX);
}

export function createHorizontalWheelNavigationTracker(
  options: Partial<WheelNavigationOptions> = {},
) {
  const resolved = { ...DEFAULT_OPTIONS, ...options };
  let accumulatedX = 0;
  let lastWheelAt = Number.NEGATIVE_INFINITY;
  let lastTriggerAt = Number.NEGATIVE_INFINITY;

  return {
    reset() {
      accumulatedX = 0;
      lastWheelAt = Number.NEGATIVE_INFINITY;
    },
    handleWheel(event: WheelEvent): WheelNavigationGesture | null {
      if (!isPageNavigationWheelCandidate(event, resolved)) {
        this.reset();
        return null;
      }

      const now = event.timeStamp;
      if (now - lastTriggerAt < resolved.debounceMs) {
        this.reset();
        lastWheelAt = now;
        return null;
      }

      const shouldStartNewGesture =
        !Number.isFinite(lastWheelAt) ||
        now - lastWheelAt > resolved.gestureTimeoutMs ||
        Math.sign(accumulatedX) !== Math.sign(event.deltaX);

      if (shouldStartNewGesture) {
        accumulatedX = 0;
      }

      accumulatedX += event.deltaX;
      lastWheelAt = now;

      if (Math.abs(accumulatedX) < resolved.triggerThreshold) return null;

      const gesture = wheelGestureFromDeltaX(accumulatedX);
      accumulatedX = 0;
      lastTriggerAt = now;
      return gesture;
    },
  };
}

export function useHorizontalWheelNavigation() {
  const { canGoBack, canGoForward, goBack, goForward } = useTabHistory();
  const navigationRef = useRef({
    canGoBack,
    canGoForward,
    goBack,
    goForward,
  });

  navigationRef.current = { canGoBack, canGoForward, goBack, goForward };

  useEffect(() => {
    if (window.desktopAPI.appInfo.os !== "macos") return;

    const tracker = createHorizontalWheelNavigationTracker();
    const handleWheel = (event: WheelEvent) => {
      if (!document.hasFocus()) {
        tracker.reset();
        return;
      }

      const gesture = tracker.handleWheel(event);
      if (!gesture) return;

      const navigation = navigationRef.current;
      if (gesture === "back" && navigation.canGoBack) {
        event.preventDefault();
        navigation.goBack();
      } else if (gesture === "forward" && navigation.canGoForward) {
        event.preventDefault();
        navigation.goForward();
      }
    };

    window.addEventListener("wheel", handleWheel, { passive: false });
    return () => {
      window.removeEventListener("wheel", handleWheel);
    };
  }, []);
}
