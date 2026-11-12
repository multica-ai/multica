/**
 * Foreground / reconnect refresh helpers.
 *
 * Most mobile caches are refreshed by realtime events, and the chat caches
 * deliberately use `staleTime: Infinity`. A missed WebSocket event would
 * otherwise leave those caches stale forever. On the two moments we know the
 * client may have missed events — the app returning to the foreground and the
 * network coming back — we invalidate the queries that currently have a
 * mounted observer. Invalidation makes them stale once, so the active screen
 * refetches without changing any per-query `staleTime`.
 *
 * `refetchType: "active"` deliberately leaves inactive cached queries alone:
 * they will refresh from their own `staleTime` / next mount, and a reconnect
 * must not turn into a workspace-wide network sweep.
 */
import type { QueryClient } from "@tanstack/react-query";

export const DEFAULT_REFRESH_COALESCE_MS = 800;

export function refreshActiveQueries(queryClient: QueryClient): Promise<void> {
  return queryClient.invalidateQueries({ refetchType: "active" });
}

/**
 * Collapse refresh triggers that arrive together. Returning to the foreground
 * usually fires AppState and NetInfo within the same frame; the two signals
 * describe one catch-up moment, not two.
 */
export function createCoalescedRefresher({
  refresh,
  now = () => Date.now(),
  minIntervalMs = DEFAULT_REFRESH_COALESCE_MS,
}: {
  refresh: () => void;
  now?: () => number;
  minIntervalMs?: number;
}): () => boolean {
  let lastRefreshAt = Number.NEGATIVE_INFINITY;

  return () => {
    const current = now();
    if (current - lastRefreshAt < minIntervalMs) return false;
    lastRefreshAt = current;
    refresh();
    return true;
  };
}
