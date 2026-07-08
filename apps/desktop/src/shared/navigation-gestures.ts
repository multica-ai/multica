export const NAVIGATION_GESTURE_CHANNEL = "navigation:gesture";

export type NavigationGesture = "back" | "forward";

export function isNavigationGesture(value: unknown): value is NavigationGesture {
  return value === "back" || value === "forward";
}

export function navigationGestureFromSwipe(
  direction: string,
): NavigationGesture | null {
  if (direction === "right") return "back";
  if (direction === "left") return "forward";
  return null;
}
