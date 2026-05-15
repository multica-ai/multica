"use client";

import { useIsNavigating } from "../navigation";

// 1px top-of-content progress bar shown while a transition-wrapped
// push/replace is mid-flight. Indeterminate by design — we don't know
// when the next route will commit, just that it's coming.
export function NavigationProgress() {
  const isNavigating = useIsNavigating();
  if (!isNavigating) return null;
  return (
    <div
      aria-hidden
      className="pointer-events-none absolute inset-x-0 top-0 z-50 h-px overflow-hidden"
    >
      <div className="h-full w-1/3 animate-nav-progress-sweep bg-primary" />
    </div>
  );
}
