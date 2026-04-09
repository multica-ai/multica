"use client";

import { create } from "zustand";
import type { AgentRuntime } from "@/shared/types";
import { api } from "@/shared/api";
import { useWorkspaceStore } from "@/features/workspace";

interface RuntimeState {
  runtimes: AgentRuntime[];
  selectedId: string;
  fetching: boolean;
}

interface RuntimeActions {
  fetchRuntimes: () => Promise<void>;
  setSelectedId: (id: string) => void;
  /** Patch a single runtime in-place (e.g. status/last_seen_at from WS event). */
  patchRuntime: (id: string, updates: Partial<AgentRuntime>) => void;
  /** Replace the full runtimes list (used on daemon:register events). */
  setRuntimes: (runtimes: AgentRuntime[]) => void;
}

type RuntimeStore = RuntimeState & RuntimeActions;

export const useRuntimeStore = create<RuntimeStore>((set, get) => ({
  // State
  runtimes: [],
  selectedId: "",
  fetching: true,

  // Actions
  fetchRuntimes: async () => {
    const workspace = useWorkspaceStore.getState().workspace;
    if (!workspace) return;
    try {
      const data = await api.listRuntimes({ workspace_id: workspace.id });
      const { selectedId } = get();
      set({
        runtimes: data,
        fetching: false,
        // Auto-select first if nothing selected
        selectedId: selectedId && data.some((r) => r.id === selectedId)
          ? selectedId
          : data[0]?.id ?? "",
      });
    } catch {
      set({ fetching: false });
    }
  },

  setSelectedId: (id) => set({ selectedId: id }),

  patchRuntime: (id, updates) => {
    set((state) => ({
      runtimes: state.runtimes.map((r) =>
        r.id === id ? { ...r, ...updates } : r,
      ),
    }));
  },

  setRuntimes: (runtimes) => {
    const { selectedId } = get();
    set({
      runtimes,
      selectedId: selectedId && runtimes.some((r) => r.id === selectedId)
        ? selectedId
        : runtimes[0]?.id ?? "",
    });
  },
}));
