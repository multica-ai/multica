import { create } from "zustand";
import { persist, createJSONStorage } from "zustand/middleware";
import { defaultStorage } from "../../platform/storage";
import type { TimelineSortDirection } from "../timeline-sort";

/**
 * User preference for issue comment timeline sort order (web/desktop).
 *
 * Persisted to localStorage so the choice follows the user across issues
 * and reloads on the same device. Default is "oldest" (oldest first, the
 * historical behaviour). The mobile app uses a separate expo-secure-store
 * hook; the two stores are intentionally not shared (platform isolation
 * matches other reading preferences).
 *
 * The query cache is always kept in canonical ASC order — only the display
 * layer reads this direction to reverse top-level groups/rows.
 */
interface TimelineSortStore {
  direction: TimelineSortDirection;
  setDirection: (direction: TimelineSortDirection) => void;
  toggle: () => void;
}

export const useTimelineSortStore = create<TimelineSortStore>()(
  persist(
    (set) => ({
      direction: "oldest",
      setDirection: (direction) => set({ direction }),
      toggle: () =>
        set((s) => ({ direction: s.direction === "oldest" ? "newest" : "oldest" })),
    }),
    {
      name: "multica_timeline_sort",
      // packages/core must not touch `localStorage` directly (SSR + platform
      // boundary); go through the SSR-safe StorageAdapter used by every other
      // core persisted store. It wraps localStorage on web/electron and is a
      // no-op on the server.
      storage: createJSONStorage(() => defaultStorage),
      // Defend against corrupted/unknown stored values: only accept the two
      // known directions, otherwise fall back to "oldest".
      merge: (persisted, current) => {
        const p = (persisted ?? {}) as Partial<TimelineSortStore>;
        const direction =
          p.direction === "newest" || p.direction === "oldest"
            ? p.direction
            : current.direction;
        return { ...current, direction };
      },
    },
  ),
);
