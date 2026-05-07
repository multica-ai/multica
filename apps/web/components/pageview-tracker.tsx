"use client";

import { useEffect } from "react";
import { usePathname, useSearchParams } from "next/navigation";
import { capturePageview } from "@multica/core/analytics";

/**
 * Fires a PostHog $pageview whenever the Next.js App Router path or query
 * string changes. Mounted once at the root so every route transition is
 * covered, including transitions into workspace-scoped subtrees.
 *
 * PostHog's own `capture_pageview: true` auto-capture is deliberately
 * disabled in `initAnalytics` so we own the event shape — this component
 * is what actually fires the event. Before this existed the acquisition
 * funnel's `/ → signup` step was empty.
 */
export function PageviewTracker() {
  const pathname = usePathname();
  const searchParams = useSearchParams();

  useEffect(() => {
    if (!pathname) return;
    const qs = searchParams?.toString();
    const url = qs ? `${pathname}?${qs}` : pathname;
    capturePageview(url);
  }, [pathname, searchParams]);

  return null;
}
