import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from "../platform/workspace-storage";
import { defaultStorage } from "../platform/storage";

/** Per-workspace, persisted set of repo URLs whose Ship Hub section is collapsed. */
interface CollapsedReposState {
  activeReleasesCollapsed: boolean;
  collapsed: Set<string>;
  toggleActiveReleases: () => void;
  isCollapsed: (repoUrl: string) => boolean;
  toggle: (repoUrl: string) => void;
  setCollapsed: (repoUrl: string, value: boolean) => void;
  reset: () => void;
}

export const useCollapsedRepos = create<CollapsedReposState>()(
  persist(
    (set, get) => ({
      activeReleasesCollapsed: false,
      collapsed: new Set<string>(),
      toggleActiveReleases: () =>
        set((s) => ({
          activeReleasesCollapsed: !s.activeReleasesCollapsed,
        })),
      isCollapsed: (repoUrl) => get().collapsed.has(repoUrl),
      toggle: (repoUrl) =>
        set((s) => {
          const next = new Set(s.collapsed);
          if (next.has(repoUrl)) next.delete(repoUrl);
          else next.add(repoUrl);
          return { collapsed: next };
        }),
      setCollapsed: (repoUrl, value) =>
        set((s) => {
          const next = new Set(s.collapsed);
          if (value) next.add(repoUrl);
          else next.delete(repoUrl);
          return { collapsed: next };
        }),
      reset: () => set({ collapsed: new Set<string>() }),
    }),
    {
      name: "multica_ship_collapsed_repos",
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

registerForWorkspaceRehydration(() => useCollapsedRepos.persist.rehydrate());
