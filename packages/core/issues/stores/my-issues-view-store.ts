"use client";

import { createStore, type StoreApi } from "zustand/vanilla";
import { persist } from "zustand/middleware";
import {
  type IssueViewState,
  viewStoreSlice,
  viewStorePersistOptions,
} from "./view-store";
import { registerForWorkspaceRehydration } from "../../platform/workspace-storage";

export type MyIssuesScope = "assigned" | "created" | "agents";

export interface MyIssuesViewState extends IssueViewState {
  scope: MyIssuesScope;
  setScope: (scope: MyIssuesScope) => void;
}

const basePersist = viewStorePersistOptions("multica_my_issues_view");

const _myIssuesViewStore = createStore<MyIssuesViewState>()(
  persist(
    (set) => ({
      ...viewStoreSlice(set as unknown as StoreApi<IssueViewState>["setState"]),
      scope: "assigned" as MyIssuesScope,
      setScope: (scope: MyIssuesScope) => set({ scope }),
    }),
    {
      name: basePersist.name,
      storage: basePersist.storage,
      partialize: (state: MyIssuesViewState) => ({
        ...basePersist.partialize(state),
        scope: state.scope,
      }),
    },
  ),
);

export const myIssuesViewStore: StoreApi<MyIssuesViewState> = _myIssuesViewStore;

registerForWorkspaceRehydration(() => _myIssuesViewStore.persist.rehydrate());
