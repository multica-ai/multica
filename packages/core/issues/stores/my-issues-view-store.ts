"use client";

import { createStore, type StoreApi } from "zustand/vanilla";
import { persist } from "zustand/middleware";
import {
  type IssueViewState,
  viewStoreSlice,
  viewStorePersistOptions,
  mergeViewStatePersisted,
} from "./view-store";
import { registerForWorkspaceRehydration } from "../../platform/workspace-storage";

export type MyIssuesScope = "all" | "assigned" | "created" | "agents";
export type MyIssuesRelation = "all" | "assigned" | "created" | "involved";

// "agents" is the user-facing scope label; the issue APIs call the same
// ownership/squad-membership relation "involved".
export function myIssuesRelationFromScope(
  scope: MyIssuesScope,
): MyIssuesRelation {
  if (scope === "agents") return "involved";
  return scope;
}

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
      // Reuse the same deep-merge as the base view store so newly added
      // cardProperties toggles inherit defaults for existing users. Without
      // this, the my-issues page renders no labels because the persisted
      // snapshot predates the `labels` key and shallow-merge wins.
      merge: mergeViewStatePersisted<MyIssuesViewState>,
    },
  ),
);

export const myIssuesViewStore: StoreApi<MyIssuesViewState> = _myIssuesViewStore;

registerForWorkspaceRehydration(() => _myIssuesViewStore.persist.rehydrate());
