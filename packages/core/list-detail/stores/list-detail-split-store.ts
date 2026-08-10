import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

/**
 * Pixel bounds of the left list rail in the expanded split layout. The list
 * panel is `minSize={240} maxSize={480}` in react-resizable-panels pixel
 * units; the store clamps on every write so a stale persisted value can never
 * push the rail outside the draggable range.
 */
export const LIST_DETAIL_RAIL_MIN_SIZE = 240;
export const LIST_DETAIL_RAIL_MAX_SIZE = 480;

function clampSize(size: number): number {
  return Math.min(
    LIST_DETAIL_RAIL_MAX_SIZE,
    Math.max(LIST_DETAIL_RAIL_MIN_SIZE, Math.round(size)),
  );
}

/**
 * Left list rail state shared by the Autopilots and Agents detail pages'
 * two-column layout. Persisted per workspace so each workspace remembers
 * whether the user collapsed the rail and how wide they dragged it.
 */
export interface ListDetailSplitState {
  /** True when the left list rail is collapsed to a narrow strip. */
  collapsed: boolean;
  /** Pixel width of the expanded rail; absent until the user drags. */
  size?: number;
  setCollapsed: (collapsed: boolean) => void;
  setSize: (size: number) => void;
  toggleCollapsed: () => void;
}

export const useListDetailSplitStore = create<ListDetailSplitState>()(
  persist(
    (set) => ({
      collapsed: true,
      size: undefined,
      setCollapsed: (collapsed) => set({ collapsed }),
      setSize: (size) => set({ size: clampSize(size) }),
      toggleCollapsed: () => set((s) => ({ collapsed: !s.collapsed })),
    }),
    {
      name: "multica_list_detail_split",
      storage: createJSONStorage(() =>
        createWorkspaceAwareStorage(defaultStorage),
      ),
      // Re-clamp on rehydration: values persisted before a bounds change (or
      // written by an older build) must land inside the draggable range.
      merge: (persisted, current) => {
        const p = (persisted ?? {}) as Partial<ListDetailSplitState>;
        return {
          ...current,
          ...p,
          size: typeof p.size === "number" ? clampSize(p.size) : undefined,
        };
      },
    },
  ),
);

registerForWorkspaceRehydration(() =>
  useListDetailSplitStore.persist.rehydrate(),
);
