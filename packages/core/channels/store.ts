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
   * In-flight tasks keyed by channel_id. Multiple agents can run concurrently
   * for the same channel (e.g. when multiple agents are @mentioned).
   * Cleared individually on task:completed/failed/cancelled.
   */
  pendingTasks: Record<string, ChannelPendingTask[]>;
  /** Set the active channel. */
  setActiveChannel: (id: string | null) => void;
  /** Toggle sidebar visibility. */
  toggleSidebar: () => void;
  setSidebarOpen: (open: boolean) => void;
  /** Add or update a pending task for a channel (by task_id). */
  setPendingTask: (channelId: string, task: ChannelPendingTask) => void;
  /** Remove a specific pending task by task_id. Removes the channel key when empty. */
  clearPendingTask: (channelId: string, taskId?: string) => void;
}

export const useChannelStore = create<ChannelUIState>((set) => ({
  activeChannelId: null,
  sidebarOpen: false,
  pendingTasks: {},
  setActiveChannel: (id) => set({ activeChannelId: id }),
  toggleSidebar: () => set((s) => ({ sidebarOpen: !s.sidebarOpen })),
  setSidebarOpen: (open) => set({ sidebarOpen: open }),
  setPendingTask: (channelId, task) =>
    set((s) => {
      const existing = s.pendingTasks[channelId] ?? [];
      // Update existing task or append new one
      const idx = existing.findIndex((t) => t.task_id === task.task_id);
      const next = idx >= 0
        ? existing.map((t, i) => (i === idx ? task : t))
        : [...existing, task];
      return { pendingTasks: { ...s.pendingTasks, [channelId]: next } };
    }),
  clearPendingTask: (channelId, taskId) =>
    set((s) => {
      const existing = s.pendingTasks[channelId];
      if (!existing) return s;
      if (!taskId) {
        // Legacy: clear all tasks for this channel
        const next = { ...s.pendingTasks };
        delete next[channelId];
        return { pendingTasks: next };
      }
      const filtered = existing.filter((t) => t.task_id !== taskId);
      if (filtered.length === 0) {
        const next = { ...s.pendingTasks };
        delete next[channelId];
        return { pendingTasks: next };
      }
      return { pendingTasks: { ...s.pendingTasks, [channelId]: filtered } };
    }),
}));
