import { create } from "zustand";
import type { Workspace, StorageAdapter } from "../types";
import type { ApiClient } from "../api/client";
import { createLogger } from "../logger";
import { setCurrentWorkspaceId, rehydrateAllWorkspaceStores } from "../platform/workspace-storage";

const logger = createLogger("workspace-store");

interface WorkspaceStoreOptions {
  storage?: StorageAdapter;
  onError?: (message: string) => void;
}

interface WorkspaceState {
  workspace: Workspace | null;
  workspaces: Workspace[];
}

interface WorkspaceActions {
  hydrateWorkspace: (
    wsList: Workspace[],
    preferredWorkspaceId?: string | null,
  ) => Workspace | null;
  switchWorkspace: (workspaceId: string) => void;
  refreshWorkspaces: () => Promise<Workspace[]>;
  updateWorkspace: (ws: Workspace) => void;
  createWorkspace: (data: {
    name: string;
    slug: string;
    description?: string;
  }) => Promise<Workspace>;
  leaveWorkspace: (workspaceId: string) => Promise<void>;
  deleteWorkspace: (workspaceId: string) => Promise<void>;
  clearWorkspace: () => void;
}

export type WorkspaceStore = WorkspaceState & WorkspaceActions;

export function createWorkspaceStore(api: ApiClient, options?: WorkspaceStoreOptions) {
  const storage = options?.storage;
  const onError = options?.onError;

  return create<WorkspaceStore>((set, get) => ({
    // State
    workspace: null,
    workspaces: [],

    // Actions
    hydrateWorkspace: (wsList, preferredWorkspaceId) => {
      set({ workspaces: wsList });

      const nextWorkspace =
        (preferredWorkspaceId
          ? wsList.find((item) => item.id === preferredWorkspaceId)
          : null) ??
        wsList[0] ??
        null;

      if (!nextWorkspace) {
        api.setWorkspaceId(null);
        setCurrentWorkspaceId(null);
        rehydrateAllWorkspaceStores();
        storage?.removeItem("multica_workspace_id");
        set({ workspace: null });
        return null;
      }

      api.setWorkspaceId(nextWorkspace.id);
      setCurrentWorkspaceId(nextWorkspace.id);
      rehydrateAllWorkspaceStores();
      storage?.setItem("multica_workspace_id", nextWorkspace.id);
      set({ workspace: nextWorkspace });
      logger.debug("hydrate workspace", nextWorkspace.name, nextWorkspace.id);

      // Members, agents, skills, issues, inbox are all managed by TanStack Query.
      // They auto-fetch when components mount with the workspace ID in their query key.

      return nextWorkspace;
    },

    switchWorkspace: (workspaceId) => {
      logger.info("switching to", workspaceId);
      const { workspaces, hydrateWorkspace } = get();
      const ws = workspaces.find((item) => item.id === workspaceId);
      if (!ws) return;

      api.setWorkspaceId(ws.id);
      storage?.setItem("multica_workspace_id", ws.id);

      // All data caches (issues, inbox, members, agents, skills, runtimes)
      // are managed by TanStack Query, keyed by wsId — auto-refetch on switch.
      set({ workspace: ws });

      hydrateWorkspace(workspaces, ws.id);
    },

    refreshWorkspaces: async () => {
      const { workspace, hydrateWorkspace } = get();
      const storedWorkspaceId = storage?.getItem("multica_workspace_id") ?? null;
      try {
        const wsList = await api.listWorkspaces();
        hydrateWorkspace(wsList, workspace?.id ?? storedWorkspaceId);
        return wsList;
      } catch (e) {
        logger.error("failed to refresh workspaces", e);
        onError?.("Failed to refresh workspaces");
        return get().workspaces;
      }
    },

    updateWorkspace: (ws) => {
      set((state) => ({
        workspace: state.workspace?.id === ws.id ? ws : state.workspace,
        workspaces: state.workspaces.map((item) =>
          item.id === ws.id ? ws : item,
        ),
      }));
    },

    createWorkspace: async (data) => {
      const ws = await api.createWorkspace(data);
      set((state) => ({ workspaces: [...state.workspaces, ws] }));
      return ws;
    },

    leaveWorkspace: async (workspaceId) => {
      await api.leaveWorkspace(workspaceId);
      const { workspace, hydrateWorkspace } = get();
      const wsList = await api.listWorkspaces();
      const preferredWorkspaceId =
        workspace?.id === workspaceId ? null : (workspace?.id ?? null);
      hydrateWorkspace(wsList, preferredWorkspaceId);
    },

    deleteWorkspace: async (workspaceId) => {
      await api.deleteWorkspace(workspaceId);
      const { workspace, hydrateWorkspace } = get();
      const wsList = await api.listWorkspaces();
      const preferredWorkspaceId =
        workspace?.id === workspaceId ? null : (workspace?.id ?? null);
      hydrateWorkspace(wsList, preferredWorkspaceId);
    },

    clearWorkspace: () => {
      api.setWorkspaceId(null);
      setCurrentWorkspaceId(null);
      rehydrateAllWorkspaceStores();
      set({ workspace: null, workspaces: [] });
    },
  }));
}
