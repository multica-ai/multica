import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";

export type ProjectTab = "overview" | "issues";

export function isProjectTab(value: unknown): value is ProjectTab {
  return value === "overview" || value === "issues";
}

// Older entries are dropped past this many projects so the saved map stays small.
const MAX_REMEMBERED = 100;

interface ProjectTabState {
  /** The tab each project was last left on, keyed by project id (ids are globally unique). */
  lastTab: Record<string, ProjectTab>;
  remember: (projectId: string, tab: ProjectTab) => void;
}

// A personal navigation habit, saved on this device like the issue opening mode.
export const useProjectTabStore = create<ProjectTabState>()(
  persist(
    (set) => ({
      lastTab: {},
      remember: (projectId, tab) =>
        set((state) => {
          if (state.lastTab[projectId] === tab) return state;
          const { [projectId]: _previous, ...rest } = state.lastTab;
          const entries = Object.entries({ ...rest, [projectId]: tab });
          return { lastTab: Object.fromEntries(entries.slice(-MAX_REMEMBERED)) };
        }),
    }),
    {
      name: "multica_project_tab",
      storage: createJSONStorage(() => defaultStorage),
      partialize: ({ lastTab }) => ({ lastTab }),
      merge: (persisted, current) => {
        const saved = persisted && typeof persisted === "object" && "lastTab" in persisted ? persisted.lastTab : null;
        const lastTab: Record<string, ProjectTab> = {};
        if (saved && typeof saved === "object") {
          for (const [id, tab] of Object.entries(saved)) if (isProjectTab(tab)) lastTab[id] = tab;
        }
        return { ...current, lastTab };
      },
    },
  ),
);

/**
 * The tab a project opens on when the URL doesn't name one: the tab this
 * person last left it on, else Overview. A project without issues opens on
 * Overview, since its Issues tab would only show an empty state.
 */
export function defaultProjectTab(remembered: ProjectTab | undefined, issueCount: number): ProjectTab {
  if (issueCount === 0) return "overview";
  return remembered ?? "overview";
}
