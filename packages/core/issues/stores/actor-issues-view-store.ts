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

export type ActorIssuesScope = "assigned" | "created";

export interface ActorIssuesViewState extends IssueViewState {
  scope: ActorIssuesScope;
  setScope: (scope: ActorIssuesScope) => void;
}

const basePersist = viewStorePersistOptions("multica_actor_issues_view");

const _actorIssuesViewStore = createStore<ActorIssuesViewState>()(
  persist(
    (set) => ({
      ...viewStoreSlice(set as unknown as StoreApi<IssueViewState>["setState"]),
      scope: "assigned" as ActorIssuesScope,
      setScope: (scope: ActorIssuesScope) => set({ scope }),
    }),
    {
      name: basePersist.name,
      storage: basePersist.storage,
      partialize: (state: ActorIssuesViewState) => ({
        ...basePersist.partialize(state),
        scope: state.scope,
      }),
      merge: mergeViewStatePersisted<ActorIssuesViewState>,
    },
  ),
);

export const actorIssuesViewStore: StoreApi<ActorIssuesViewState> =
  _actorIssuesViewStore;

registerForWorkspaceRehydration(() =>
  _actorIssuesViewStore.persist.rehydrate(),
);
