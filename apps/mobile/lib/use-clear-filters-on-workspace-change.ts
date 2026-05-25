/**
 * Run a clear-filters callback when the active workspace id transitions
 * between two *defined* values. Mirrors web's `useClearFiltersOnWorkspaceChange`
 * (packages/core/issues/stores/view-store.ts:273-284) — the ref guard skips
 * the first render so initial state isn't wiped on mount, and skips the
 * null → uuid hydration on workspace load.
 *
 * Consumers: any screen whose view-store carries workspace-scoped filter
 * state (my-issues, all-issues, future projects/inbox filters). The hook
 * doesn't import any specific store — the caller passes a stable `clearFn`
 * (typically `useFooViewStore.getState().clearFilters`) so the hook stays
 * store-agnostic.
 */
import { useEffect, useRef } from "react";

export function useClearFiltersOnWorkspaceChange(
  clearFn: () => void,
  wsId: string | null,
) {
  const prevRef = useRef<string | null>(null);
  useEffect(() => {
    if (prevRef.current && wsId && wsId !== prevRef.current) {
      clearFn();
    }
    prevRef.current = wsId ?? null;
  }, [wsId, clearFn]);
}
