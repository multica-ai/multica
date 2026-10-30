"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

export type IssuesScope = "all" | "members" | "agents";

/**
 * Page identity for the assignee-type tab. Every surface remembers its own
 * tab — the Issues page under "issues", each project page under
 * `project:<id>` — so switching tabs inside one project never drags the
 * Issues page (or another project) along with it.
 */
export type IssuesScopePageKey = "issues" | `project:${string}`;

interface IssuesScopeState {
  scopes: Partial<Record<IssuesScopePageKey, IssuesScope>>;
  setScope: (page: IssuesScopePageKey, scope: IssuesScope) => void;
}

export const useIssuesScopeStore = create<IssuesScopeState>()(
  persist(
    (set) => ({
      scopes: {},
      setScope: (page, scope) =>
        set((state) => ({ scopes: { ...state.scopes, [page]: scope } })),
    }),
    {
      name: "multica_issues_scope",
      version: 1,
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      migrate: (persisted, version) => {
        // v0 stored one tab shared by every page; carry it over as the
        // Issues page's tab and let project pages start fresh on "all".
        if (version === 0) {
          const legacy = (persisted as { scope?: IssuesScope } | undefined)
            ?.scope;
          return {
            scopes:
              legacy === "members" || legacy === "agents"
                ? { issues: legacy }
                : {},
          } as IssuesScopeState;
        }
        return persisted as IssuesScopeState;
      },
    },
  ),
);

/** The page's current tab; "all" until the user picks one. */
export function useIssuesScope(page: IssuesScopePageKey): IssuesScope {
  return useIssuesScopeStore((s) => s.scopes[page] ?? "all");
}

registerForWorkspaceRehydration(() => useIssuesScopeStore.persist.rehydrate());
