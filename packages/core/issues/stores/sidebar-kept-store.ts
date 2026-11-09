import { create } from "zustand";

/** The open item a sidebar list showed, and its place there. */
export interface SidebarKeptItem {
  id: string;
  index: number;
}

interface SidebarKeptState {
  /** By list: a sidebar source, or `projects` for the Projects section. */
  held: Record<string, SidebarKeptItem>;
  /** Records that `list` shows the open item `id` at `index`. */
  hold: (list: string, id: string, index: number) => void;
  /** Forgets every place held for an item that is no longer open. */
  release: (openIds: readonly string[]) => void;
}

/**
 * Where the sidebar lists last showed the open issue or project, so a list
 * can keep that row in place after an edit filters it out. Shared, so the
 * panel and its hover peek keep the same rows.
 */
export const useSidebarKeptStore = create<SidebarKeptState>()((set) => ({
  held: {},
  hold: (list, id, index) =>
    set((s) => {
      const current = s.held[list];
      if (current?.id === id && current.index === index) return s;
      return { held: { ...s.held, [list]: { id, index } } };
    }),
  release: (openIds) =>
    set((s) => {
      const kept = Object.entries(s.held).filter(([, item]) => openIds.includes(item.id));
      if (kept.length === Object.keys(s.held).length) return s;
      return { held: Object.fromEntries(kept) };
    }),
}));
