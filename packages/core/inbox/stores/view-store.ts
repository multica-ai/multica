import { create } from "zustand";
import { persist } from "zustand/middleware";
import {
  viewStorePersistOptions,
  viewStoreSlice,
  type IssueViewState,
} from "../../issues/stores/view-store";
import { registerForWorkspaceRehydration } from "../../platform/workspace-storage";

export const useInboxViewStore = create<IssueViewState>()(
  persist(viewStoreSlice, viewStorePersistOptions("multica_inbox_view"))
);

registerForWorkspaceRehydration(() => useInboxViewStore.persist.rehydrate());
