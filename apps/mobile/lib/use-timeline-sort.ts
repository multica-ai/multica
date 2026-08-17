/**
 * Timeline sort preference for the mobile issue screen.
 *
 * Mirrors web's `useTimelineSortStore` but persists via expo-secure-store
 * (same pattern as use-color-scheme.ts). The value is a single
 * "oldest" | "newest" string, well under secure-store's 2KB per-key limit.
 *
 * `hydrated` gates the first mount of TimelineList: secure-store is async,
 * so without the gate a "newest" user would see one frame of "oldest"
 * rendering before the preference resolves — particularly visible with a
 * warm query cache where timeline data is present at mount. Consumers
 * should render a loading placeholder until `hydrated === true`.
 *
 * Web and mobile stores are intentionally not shared (platform isolation
 * matches other reading preferences like theme).
 */
import { useCallback, useEffect, useState } from "react";
import * as SecureStore from "expo-secure-store";
import type { TimelineSortDirection } from "@multica/core/issues/timeline-sort";

const STORAGE_KEY = "timeline-sort-direction";
const DEFAULT_DIRECTION: TimelineSortDirection = "oldest";

function isDirection(value: unknown): value is TimelineSortDirection {
  return value === "oldest" || value === "newest";
}

export function useTimelineSort() {
  const [direction, setDirectionState] =
    useState<TimelineSortDirection>(DEFAULT_DIRECTION);
  const [hydrated, setHydrated] = useState(false);

  useEffect(() => {
    let cancelled = false;
    SecureStore.getItemAsync(STORAGE_KEY)
      .then((saved) => {
        if (cancelled) return;
        if (isDirection(saved)) setDirectionState(saved);
      })
      .catch(() => {
        // Keychain/Keystore failures are non-fatal; keep the default.
      })
      .finally(() => {
        if (!cancelled) setHydrated(true);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const setDirection = useCallback((next: TimelineSortDirection) => {
    setDirectionState(next);
    void SecureStore.setItemAsync(STORAGE_KEY, next).catch(() => {});
  }, []);

  const toggle = useCallback(() => {
    setDirectionState((prev) => {
      const next: TimelineSortDirection =
        prev === "oldest" ? "newest" : "oldest";
      void SecureStore.setItemAsync(STORAGE_KEY, next).catch(() => {});
      return next;
    });
  }, []);

  return { direction, hydrated, setDirection, toggle };
}
