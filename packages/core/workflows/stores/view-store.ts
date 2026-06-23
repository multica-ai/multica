"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

export type WorkflowViewMode = "panorama" | "overview" | "editor";

interface WorkflowViewState {
  viewMode: WorkflowViewMode;
  setViewMode: (mode: WorkflowViewMode) => void;
}

/** Global singleton for the workflow detail page. */
export const useWorkflowViewStore = create<WorkflowViewState>()(
  persist(
    (set) => ({
      viewMode: "panorama" as WorkflowViewMode,
      setViewMode: (mode: WorkflowViewMode) => set({ viewMode: mode }),
    }),
    {
      name: "multica_workflows_view",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
    },
  ),
);

registerForWorkspaceRehydration(() => useWorkflowViewStore.persist.rehydrate());
