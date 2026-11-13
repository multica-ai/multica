import { create } from "zustand";
import { GitHubConnectionEventSchema } from "./connection-event";

interface GitHubConnection {
  workspaceID: string;
  connectionID?: string;
  installationID: string;
}

interface GitHubConnectionState {
  connection: GitHubConnection | null;
  begin: (workspaceID: string, connectionID?: string) => void;
  complete: (payload: unknown) => void;
  reset: (workspaceID?: string) => void;
}

// This window's transient handoff survives settings navigation. Installation
// data stays in Query; only the initiated flow and its completion pointer live here.
export const useGitHubConnectionStore = create<GitHubConnectionState>((set) => ({
  connection: null,
  begin: (workspaceID, connectionID) => set({
    connection: { workspaceID, connectionID, installationID: "" },
  }),
  complete: (payload) => {
    const event = GitHubConnectionEventSchema.safeParse(payload);
    if (!event.success) return;
    const { installation, connection_id: connectionID } = event.data;
    set((state) => {
      const pending = state.connection;
      if (
        !pending || pending.installationID ||
        pending.workspaceID !== installation.workspace_id ||
        (pending.connectionID && pending.connectionID !== connectionID)
      ) return state;
      return { connection: { ...pending, installationID: installation.id } };
    });
  },
  reset: (workspaceID) => set((state) =>
    workspaceID && state.connection?.workspaceID !== workspaceID
      ? state
      : { connection: null },
  ),
}));
