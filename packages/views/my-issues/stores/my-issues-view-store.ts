"use client";

import { createStore, type StoreApi } from "zustand/vanilla";
import { persist } from "zustand/middleware";
import {
  type IssueViewState,
  viewStoreSlice,
  viewStorePersistOptions,
} from "@multica/core/issues/stores/view-store";

export type MyIssuesScope = "assigned" | "created" | "agents";

export interface MyIssuesViewState extends IssueViewState {
  scope: MyIssuesScope;
  setScope: (scope: MyIssuesScope) => void;
}

const basePersist = viewStorePersistOptions("multica_my_issues_view");

export const myIssuesViewStore: StoreApi<MyIssuesViewState> = createStore<MyIssuesViewState>()(
  persist(
    (set) => ({
      ...viewStoreSlice(set as unknown as StoreApi<IssueViewState>["setState"]),
      scope: "assigned" as MyIssuesScope,
      setScope: (scope: MyIssuesScope) => set({ scope }),
    }),
    {
      name: basePersist.name,
      partialize: (state: MyIssuesViewState) => ({
        ...basePersist.partialize(state),
        scope: state.scope,
      }),
    },
  ),
);
