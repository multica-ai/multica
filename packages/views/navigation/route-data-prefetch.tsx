"use client";

import { createContext, use } from "react";

/**
 * Warms the query cache for an in-app href. `AppLink` calls it on hover and
 * focus, alongside the adapter's route-code `prefetch`, so the destination's
 * data is usually cached by the time the click lands. The provider sits
 * inside the workspace shell; links rendered outside one get `null` and skip
 * it.
 */
export interface RouteDataPrefetcher {
  /**
   * A link to `href` is pointed at or focused. A warm-up that costs a request
   * per link may wait for a hovering pointer to rest before it starts.
   */
  prefetch(href: string, trigger: "hover" | "focus"): void;
  /** The pointer left the link: drop what has not started yet. */
  cancel(): void;
}

const RouteDataPrefetchContext = createContext<RouteDataPrefetcher | null>(
  null,
);

export const RouteDataPrefetchProvider = RouteDataPrefetchContext.Provider;

export function useRouteDataPrefetch(): RouteDataPrefetcher | null {
  return use(RouteDataPrefetchContext);
}
