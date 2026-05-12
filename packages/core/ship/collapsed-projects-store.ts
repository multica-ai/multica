import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from "../platform/workspace-storage";
import { defaultStorage } from "../platform/storage";

/** Per-workspace, persisted set of project ids whose Ship Hub section
 *  is collapsed. Lives in core (per CLAUDE.md "all stores live in
 *  packages/core") and is workspace-aware so collapse preferences for
 *  one workspace's projects don't leak into another's.
 *
 *  Bug fix — collapse state used to be local React useState in
 *  ship-project-section.tsx, which reset on every page load. Users
 *  who didn't care about a particular project (e.g. RC Mobile, an
 *  empty project) had to re-collapse it every time they opened the
 *  Ship Hub. This is a UI preference, not ephemeral state — the
 *  CLAUDE.md "don't persist ephemeral UI state" rule does not apply
 *  here: a collapse choice is the same kind of preference as the
 *  view-mode toggle on the project list. */
interface CollapsedProjectsState {
  activeReleasesCollapsed: boolean;
  /** projectIds collapsed in the current workspace. Set so toggle is
   *  O(1); JSON-serialised as an array via the persist middleware
   *  rehydration step below. */
  collapsed: Set<string>;
  toggleActiveReleases: () => void;
  isCollapsed: (projectId: string) => boolean;
  toggle: (projectId: string) => void;
  setCollapsed: (projectId: string, value: boolean) => void;
  reset: () => void;
}

export const useCollapsedProjects = create<CollapsedProjectsState>()(
  persist(
    (set, get) => ({
      activeReleasesCollapsed: false,
      collapsed: new Set<string>(),
      toggleActiveReleases: () =>
        set((s) => ({
          activeReleasesCollapsed: !s.activeReleasesCollapsed,
        })),
      isCollapsed: (projectId) => get().collapsed.has(projectId),
      toggle: (projectId) =>
        set((s) => {
          const next = new Set(s.collapsed);
          if (next.has(projectId)) next.delete(projectId);
          else next.add(projectId);
          return { collapsed: next };
        }),
      setCollapsed: (projectId, value) =>
        set((s) => {
          const next = new Set(s.collapsed);
          if (value) next.add(projectId);
          else next.delete(projectId);
          return { collapsed: next };
        }),
      reset: () => set({ collapsed: new Set<string>() }),
    }),
    {
      name: "multica_ship_collapsed_projects",
      storage: createJSONStorage(
        () => createWorkspaceAwareStorage(defaultStorage),
        {
          // Set isn't JSON-serialisable; round-trip through arrays.
          replacer: (_key, value) =>
            value instanceof Set ? Array.from(value) : value,
          reviver: (key, value) =>
            key === "collapsed" && Array.isArray(value)
              ? new Set<string>(value as string[])
              : value,
        },
      ),
    },
  ),
);

registerForWorkspaceRehydration(() =>
  useCollapsedProjects.persist.rehydrate(),
);
