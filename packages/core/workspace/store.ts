import { create } from "zustand";
import type { Workspace, StorageAdapter } from "../types";
import type { ApiClient } from "../api/client";
import { createLogger } from "../logger";
import { setCurrentWorkspaceId, rehydrateAllWorkspaceStores } from "../platform/workspace-storage";

const logger = createLogger("workspace-store");

interface WorkspaceStoreOptions {
  storage?: StorageAdapter;
}

interface WorkspaceState {
  workspace: Workspace | null;
}

interface WorkspaceActions {
  /**
   * Pick a workspace from a list and set it as current.
   * The list itself is NOT stored here — it lives in React Query.
   */
  hydrateWorkspace: (
    wsList: Workspace[],
    preferredWorkspaceId?: string | null,
  ) => Workspace | null;
  /** Switch to a workspace. Caller provides the full object (from React Query). */
  switchWorkspace: (ws: Workspace) => void;
  /** Update current workspace data in place (e.g. after rename). */
  updateWorkspace: (ws: Workspace) => void;
  clearWorkspace: () => void;
}

export type WorkspaceStore = WorkspaceState & WorkspaceActions;

export function createWorkspaceStore(api: ApiClient, options?: WorkspaceStoreOptions) {
  const storage = options?.storage;

  return create<WorkspaceStore>((set) => ({
    // Only the currently selected workspace (UI state).
    // The workspace list is server state and lives in React Query.
    workspace: null,

    hydrateWorkspace: (wsList, preferredWorkspaceId) => {
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

      return nextWorkspace;
    },

    switchWorkspace: (ws) => {
      logger.info("switching to", ws.id);
      api.setWorkspaceId(ws.id);
      setCurrentWorkspaceId(ws.id);
      rehydrateAllWorkspaceStores();
      storage?.setItem("multica_workspace_id", ws.id);
      set({ workspace: ws });
    },

    updateWorkspace: (ws) => {
      set((state) => ({
        workspace: state.workspace?.id === ws.id ? ws : state.workspace,
      }));
    },

    clearWorkspace: () => {
      api.setWorkspaceId(null);
      setCurrentWorkspaceId(null);
      rehydrateAllWorkspaceStores();
      set({ workspace: null });
    },
  }));
}
