"use client";

import { create } from "zustand";
import { persist } from "zustand/middleware";

export type IssuesScope = "all" | "members" | "agents";

interface IssuesScopeState {
  scope: IssuesScope;
  setScope: (scope: IssuesScope) => void;
}

export const useIssuesScopeStore = create<IssuesScopeState>()(
  persist(
    (set) => ({
      scope: "all",
      setScope: (scope) => set({ scope }),
    }),
    { name: "multica_issues_scope" },
  ),
);
