"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

// Per-workspace memory of the last agent the user picked in the Quick Create
// modal. Defaulted to that agent on next open so frequent users skip the
// picker entirely. Persisted with the workspace-aware StateStorage so
// switching workspaces shows the right default automatically. Per-user
// scoping comes for free from localStorage being browser-profile-local —
// matches how draft-store / issues-scope-store / comment-collapse-store
// already namespace themselves.

export interface QuickCreatePendingTask {
  taskId: string;
  prompt: string;
  agentId: string;
  agentName: string;
}

export type QuickCreateResult =
  | { type: "done"; issueId: string; identifier: string; title: string }
  | { type: "failed"; error: string; originalPrompt: string };

interface QuickCreateState {
  lastAgentId: string | null;
  setLastAgentId: (id: string | null) => void;
  keepOpen: boolean;
  setKeepOpen: (v: boolean) => void;

  // In-flight quick-create tasks (not persisted — ephemeral per session).
  // Keyed by task_id so the WS handler can resolve them.
  pendingTasks: Record<string, QuickCreatePendingTask>;
  addPendingTask: (task: QuickCreatePendingTask) => void;
  removePendingTask: (taskId: string) => void;
}

export const useQuickCreateStore = create<QuickCreateState>()(
  persist(
    (set) => ({
      lastAgentId: null,
      setLastAgentId: (id) => set({ lastAgentId: id }),
      keepOpen: false,
      setKeepOpen: (v) => set({ keepOpen: v }),

      pendingTasks: {},
      addPendingTask: (task) =>
        set((s) => ({
          pendingTasks: { ...s.pendingTasks, [task.taskId]: task },
        })),
      removePendingTask: (taskId) =>
        set((s) => {
          const { [taskId]: _, ...rest } = s.pendingTasks;
          return { pendingTasks: rest };
        }),
    }),
    {
      name: "multica_quick_create",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      partialize: (state) => ({
        lastAgentId: state.lastAgentId,
        keepOpen: state.keepOpen,
      }),
    },
  ),
);

registerForWorkspaceRehydration(() => useQuickCreateStore.persist.rehydrate());
