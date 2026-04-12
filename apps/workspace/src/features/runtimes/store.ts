"use client";

import { useMemo } from "react";
import { create } from "zustand";
import type { AgentRuntime } from "@/shared/types";
import { getAppQueryClient, queryKeys } from "@/shared/query";
import { useWorkspaceStore } from "@/features/workspace";
import { runtimesQueryOptions, useRuntimesQuery } from "./queries";

interface RuntimeState {
  runtimes: AgentRuntime[];
  selectedId: string;
  fetching: boolean;
}

interface RuntimeActions {
  fetchRuntimes: () => Promise<void>;
  setSelectedId: (id: string) => void;
  patchRuntime: (id: string, updates: Partial<AgentRuntime>) => void;
  setRuntimes: (runtimes: AgentRuntime[]) => void;
}

type RuntimeStore = RuntimeState & RuntimeActions;
type RuntimeStoreSelector<T> = (state: RuntimeStore) => T;

interface RuntimeStoreHook {
  <T>(selector: RuntimeStoreSelector<T>): T;
  getState: () => RuntimeStore;
}

const useRuntimeSessionStore = create<{ selectedId: string; setSelectedId: (id: string) => void }>((set) => ({
  selectedId: "",
  setSelectedId: (id) => set({ selectedId: id }),
}));

function getRuntimeSnapshot(): RuntimeStore {
  const workspace = useWorkspaceStore.getState().workspace;
  const runtimes = workspace
    ? getAppQueryClient().getQueryData<AgentRuntime[]>(queryKeys.runtimes.all(workspace.id)) ?? []
    : [];
  const selectedId = useRuntimeSessionStore.getState().selectedId;

  return {
    runtimes,
    selectedId,
    fetching: false,
    fetchRuntimes: async () => {
      const nextWorkspace = useWorkspaceStore.getState().workspace;
      if (!nextWorkspace) return;
      await getAppQueryClient().fetchQuery(runtimesQueryOptions(nextWorkspace.id));
    },
    setSelectedId: (id) => useRuntimeSessionStore.getState().setSelectedId(id),
    patchRuntime: (id, updates) => {
      const nextWorkspace = useWorkspaceStore.getState().workspace;
      if (!nextWorkspace) return;
      getAppQueryClient().setQueryData<AgentRuntime[]>(queryKeys.runtimes.all(nextWorkspace.id), (existing = []) =>
        existing.map((runtime) => (runtime.id === id ? { ...runtime, ...updates } : runtime)),
      );
    },
    setRuntimes: (nextRuntimes) => {
      const nextWorkspace = useWorkspaceStore.getState().workspace;
      if (!nextWorkspace) return;
      getAppQueryClient().setQueryData(queryKeys.runtimes.all(nextWorkspace.id), nextRuntimes);
      const currentSelectedId = useRuntimeSessionStore.getState().selectedId;
      if (!currentSelectedId || !nextRuntimes.some((runtime) => runtime.id === currentSelectedId)) {
        useRuntimeSessionStore.getState().setSelectedId(nextRuntimes[0]?.id ?? "");
      }
    },
  };
}

export const useRuntimeStore = ((selector: RuntimeStoreSelector<unknown>) => {
  const sessionState = useRuntimeSessionStore();
  const runtimesQuery = useRuntimesQuery();
  const runtimes = runtimesQuery.data ?? [];
  const selectedId = sessionState.selectedId && runtimes.some((runtime) => runtime.id === sessionState.selectedId)
    ? sessionState.selectedId
    : runtimes[0]?.id ?? "";

  const snapshot = useMemo<RuntimeStore>(
    () => ({
      ...getRuntimeSnapshot(),
      runtimes,
      selectedId,
      fetching: runtimesQuery.isPending,
    }),
    [runtimes, runtimesQuery.isPending, selectedId],
  );

  return selector(snapshot);
}) as RuntimeStoreHook;

useRuntimeStore.getState = getRuntimeSnapshot;
