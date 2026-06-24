import { create } from "zustand";
import { persist } from "zustand/middleware";

export type GroupMode = "time" | "severity" | "project" | "type";
export type ViewDensity = "compact" | "comfortable";

export interface InboxFilterState {
  groupMode: GroupMode;
  density: ViewDensity;
  unreadOnly: boolean;
  searchQuery: string;
  selectedIds: Set<string>;
  collapsedGroups: Set<string>;
  multiselectActive: boolean;

  setGroupMode: (mode: GroupMode) => void;
  setDensity: (density: ViewDensity) => void;
  toggleUnreadOnly: () => void;
  setSearchQuery: (query: string) => void;

  toggleSelect: (id: string) => void;
  selectAll: (ids: string[]) => void;
  clearSelection: () => void;
  setMultiselectActive: (active: boolean) => void;

  toggleGroupCollapse: (groupKey: string) => void;
  setGroupCollapsed: (groupKey: string, collapsed: boolean) => void;
}

export const useInboxFilterStore = create<InboxFilterState>()(
  persist(
    (set) => ({
      groupMode: "time",
      density: "comfortable",
      unreadOnly: false,
      searchQuery: "",
      selectedIds: new Set<string>(),
      collapsedGroups: new Set<string>(),
      multiselectActive: false,

      setGroupMode: (mode) => set({ groupMode: mode }),
      setDensity: (density) => set({ density }),
      toggleUnreadOnly: () => set((s) => ({ unreadOnly: !s.unreadOnly })),
      setSearchQuery: (query) => set({ searchQuery: query }),

      toggleSelect: (id) =>
        set((s) => {
          const next = new Set(s.selectedIds);
          if (next.has(id)) {
            next.delete(id);
          } else {
            next.add(id);
          }
          return { selectedIds: next };
        }),
      selectAll: (ids) => set({ selectedIds: new Set(ids) }),
      clearSelection: () => set({ selectedIds: new Set() }),
      setMultiselectActive: (active) => set({ multiselectActive: active }),

      toggleGroupCollapse: (groupKey) =>
        set((s) => {
          const next = new Set(s.collapsedGroups);
          if (next.has(groupKey)) {
            next.delete(groupKey);
          } else {
            next.add(groupKey);
          }
          return { collapsedGroups: next };
        }),
      setGroupCollapsed: (groupKey, collapsed) =>
        set((s) => {
          const next = new Set(s.collapsedGroups);
          if (collapsed) {
            next.add(groupKey);
          } else {
            next.delete(groupKey);
          }
          return { collapsedGroups: next };
        }),
    }),
    {
      name: "multica-inbox-filters",
      partialize: (state) => ({
        groupMode: state.groupMode,
        density: state.density,
      }),
      merge: (persisted, current) => ({
        ...current,
        ...(persisted as Partial<InboxFilterState>),
      }),
    },
  ),
);
