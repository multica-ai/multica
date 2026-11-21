"use client";

import { useEffect } from "react";
import { useNavigation } from "./context";

const warmed = new Set<string>();

/**
 * Prefetches one page of a kind (`"doc"`, `"issue"`) once per session when the
 * browser is idle. On web a detail page's code is shared by every page of its
 * kind but only requested after one page's route payload arrives, which put a
 * whole round trip in front of the first one opened; warming one of them here
 * loads that code early and teaches the router the route's shape. Desktop
 * bundles its routes and has no route prefetch, so this is a no-op there.
 */
export function useWarmRoute(kind: string, href: string | null | undefined) {
  const { prefetch } = useNavigation();
  useEffect(() => {
    if (!href || !prefetch || warmed.has(kind)) return;
    const warm = () => {
      // Every list of a kind mounted in one commit scheduled its own warm-up.
      if (warmed.has(kind)) return;
      warmed.add(kind);
      prefetch(href);
    };
    if (typeof window.requestIdleCallback === "function") {
      const id = window.requestIdleCallback(warm, { timeout: 3000 });
      return () => window.cancelIdleCallback(id);
    }
    const timer = window.setTimeout(warm, 1500);
    return () => window.clearTimeout(timer);
  }, [kind, href, prefetch]);
}
