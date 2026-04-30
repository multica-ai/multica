"use client";

import { useEffect } from "react";
import { create } from "zustand";
import type { StateStorage } from "zustand/middleware";
import { createJSONStorage, persist } from "zustand/middleware";
import { useAuthStore } from "../../auth";
import { defaultStorage } from "../../platform/storage";
import type { StorageAdapter, IssuePriority } from "../../types";
import type { ActorFilterValue } from "./view-store";

export interface MobileIssuesFilterState {
  priorityFilters: IssuePriority[];
  assigneeFilters: ActorFilterValue[];
  includeNoAssignee: boolean;
  creatorFilters: ActorFilterValue[];
  projectFilters: string[];
  includeNoProject: boolean;
  togglePriorityFilter: (priority: IssuePriority) => void;
  toggleAssigneeFilter: (value: ActorFilterValue) => void;
  toggleNoAssignee: () => void;
  toggleCreatorFilter: (value: ActorFilterValue) => void;
  toggleProjectFilter: (projectId: string) => void;
  toggleNoProject: () => void;
  clearFilters: () => void;
}

const emptyFilters = {
  priorityFilters: [],
  assigneeFilters: [],
  includeNoAssignee: false,
  creatorFilters: [],
  projectFilters: [],
  includeNoProject: false,
};

function currentUserId() {
  try {
    return useAuthStore.getState?.().user?.id ?? null;
  } catch {
    return null;
  }
}

function createUserAwareStorage(adapter: StorageAdapter): StateStorage {
  const resolve = (key: string) => {
    const userId = currentUserId();
    return userId ? `${key}:${userId}` : null;
  };

  return {
    getItem: (key) => {
      const resolved = resolve(key);
      return resolved ? adapter.getItem(resolved) : null;
    },
    setItem: (key, value) => {
      const resolved = resolve(key);
      if (resolved) adapter.setItem(resolved, value);
    },
    removeItem: (key) => {
      const resolved = resolve(key);
      if (resolved) adapter.removeItem(resolved);
    },
  };
}

const actorFilterEquals = (a: ActorFilterValue, b: ActorFilterValue) =>
  a.type === b.type && a.id === b.id;

export const useMobileIssuesFilterStore = create<MobileIssuesFilterState>()(
  persist(
    (set) => ({
      ...emptyFilters,
      togglePriorityFilter: (priority) =>
        set((state) => ({
          priorityFilters: state.priorityFilters.includes(priority)
            ? state.priorityFilters.filter((p) => p !== priority)
            : [...state.priorityFilters, priority],
        })),
      toggleAssigneeFilter: (value) =>
        set((state) => {
          const exists = state.assigneeFilters.some((f) => actorFilterEquals(f, value));
          return {
            assigneeFilters: exists
              ? state.assigneeFilters.filter((f) => !actorFilterEquals(f, value))
              : [...state.assigneeFilters, value],
          };
        }),
      toggleNoAssignee: () =>
        set((state) => ({ includeNoAssignee: !state.includeNoAssignee })),
      toggleCreatorFilter: (value) =>
        set((state) => {
          const exists = state.creatorFilters.some((f) => actorFilterEquals(f, value));
          return {
            creatorFilters: exists
              ? state.creatorFilters.filter((f) => !actorFilterEquals(f, value))
              : [...state.creatorFilters, value],
          };
        }),
      toggleProjectFilter: (projectId) =>
        set((state) => ({
          projectFilters: state.projectFilters.includes(projectId)
            ? state.projectFilters.filter((id) => id !== projectId)
            : [...state.projectFilters, projectId],
        })),
      toggleNoProject: () =>
        set((state) => ({ includeNoProject: !state.includeNoProject })),
      clearFilters: () => set(emptyFilters),
    }),
    {
      name: "multica_mobile_issues_filters",
      storage: createJSONStorage(() => createUserAwareStorage(defaultStorage)),
      partialize: (state) => ({
        priorityFilters: state.priorityFilters,
        assigneeFilters: state.assigneeFilters,
        includeNoAssignee: state.includeNoAssignee,
        creatorFilters: state.creatorFilters,
        projectFilters: state.projectFilters,
        includeNoProject: state.includeNoProject,
      }),
    },
  ),
);

export function useRehydrateMobileIssuesFilters(userId: string | undefined) {
  useEffect(() => {
    if (userId) {
      useMobileIssuesFilterStore.persist.rehydrate();
    } else {
      useMobileIssuesFilterStore.getState().clearFilters();
    }
  }, [userId]);
}
