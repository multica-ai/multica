import { create } from "zustand";

/**
 * A pending (in-flight) channel task — the server is running an agent for this
 * channel. We track it so the channel view can show a live task timeline.
 */
export interface ChannelPendingTask {
  task_id: string;
  agent_id: string;
  channel_id: string;
  status: "queued" | "dispatched" | "running";
}

/**
 * Channel UI state — client-side only.
 * Server state (channels, messages, members) lives in TanStack Query.
 */
export interface ChannelUIState {
  /** Currently active channel ID (viewing detail/messages). */
  activeChannelId: string | null;
  /** Whether the channel sidebar is open. */
  sidebarOpen: boolean;
  /**
   * In-flight tasks keyed by channel_id. At most one active task per channel
   * (agents process messages sequentially). Cleared on task:completed/failed/cancelled.
   */
  pendingTasks: Record<string, ChannelPendingTask>;
  /** Set the active channel. */
  setActiveChannel: (id: string | null) => void;
  /** Toggle sidebar visibility. */
  toggleSidebar: () => void;
  setSidebarOpen: (open: boolean) => void;
  /** Upsert a pending task for a channel. */
  setPendingTask: (channelId: string, task: ChannelPendingTask) => void;
  /** Remove the pending task for a channel (on completion/failure/cancel). */
  clearPendingTask: (channelId: string) => void;
}

export const useChannelStore = create<ChannelUIState>((set) => ({
  activeChannelId: null,
  sidebarOpen: false,
  pendingTasks: {},
  setActiveChannel: (id) => set({ activeChannelId: id }),
  toggleSidebar: () => set((s) => ({ sidebarOpen: !s.sidebarOpen })),
  setSidebarOpen: (open) => set({ sidebarOpen: open }),
  setPendingTask: (channelId, task) =>
    set((s) => ({ pendingTasks: { ...s.pendingTasks, [channelId]: task } })),
  clearPendingTask: (channelId) =>
    set((s) => {
      const next = { ...s.pendingTasks };
      delete next[channelId];
      return { pendingTasks: next };
    }),
}));
