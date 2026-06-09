"use client";

import { useEffect } from "react";
import { usePathname } from "next/navigation";
import { capturePageview } from "@multica/core/analytics";

/**
 * Fires a PostHog $pageview whenever the Next.js App Router pathname changes.
 * Mounted once at the root so every route transition is covered, including
 * transitions into workspace-scoped subtrees.
 *
 * Deliberately keyed on `pathname` only — NOT `useSearchParams`. Filter / sort
 * / search state lives in the query string and changes constantly on a
 * dashboard; firing a pageview on every query-string change was ~17% pure
 * noise (and billed events) with no funnel signal. The query string is also
 * dropped from the captured URL by `capturePageview` (it section-normalizes
 * the path), so OAuth `code` / `state` never reach PostHog either.
 *
 * PostHog's own `capture_pageview: true` auto-capture is deliberately disabled
 * in `initAnalytics` so this component owns the event shape.
 */
export function PageviewTracker() {
  const pathname = usePathname();

  useEffect(() => {
    if (pathname) capturePageview(pathname);
  }, [pathname]);

  return null;
}
