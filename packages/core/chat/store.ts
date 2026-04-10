import { create } from "zustand";
import type { StorageAdapter } from "../types";
import { getCurrentWorkspaceId, registerForWorkspaceRehydration } from "../platform/workspace-storage";

const AGENT_STORAGE_KEY = "multica:chat:selectedAgentId";
const SESSION_STORAGE_KEY = "multica:chat:activeSessionId";

export interface ChatTimelineItem {
  seq: number;
  type: "tool_use" | "tool_result" | "thinking" | "text" | "error";
  tool?: string;
  content?: string;
  input?: Record<string, unknown>;
  output?: string;
}

export interface ChatState {
  isOpen: boolean;
  isFullscreen: boolean;
  activeSessionId: string | null;
  pendingTaskId: string | null;
  selectedAgentId: string | null;
  showHistory: boolean;
  timelineItems: ChatTimelineItem[];
  setOpen: (open: boolean) => void;
  toggle: () => void;
  toggleFullscreen: () => void;
  setActiveSession: (id: string | null) => void;
  setPendingTask: (taskId: string | null) => void;
  setSelectedAgentId: (id: string) => void;
  setShowHistory: (show: boolean) => void;
  addTimelineItem: (item: ChatTimelineItem) => void;
  clearTimeline: () => void;
}

export interface ChatStoreOptions {
  storage: StorageAdapter;
}

export function createChatStore(options: ChatStoreOptions) {
  const { storage } = options;

  const wsKey = (base: string) => {
    const wsId = getCurrentWorkspaceId();
    return wsId ? `${base}:${wsId}` : base;
  };

  const store = create<ChatState>((set) => ({
    isOpen: false,
    isFullscreen: false,
    activeSessionId: storage.getItem(wsKey(SESSION_STORAGE_KEY)),
    pendingTaskId: null,
    selectedAgentId: storage.getItem(wsKey(AGENT_STORAGE_KEY)),
    showHistory: false,
    timelineItems: [],
    setOpen: (open) =>
      set({ isOpen: open, ...(open ? {} : { isFullscreen: false }) }),
    toggle: () =>
      set((s) => ({
        isOpen: !s.isOpen,
        ...(s.isOpen ? { isFullscreen: false } : {}),
      })),
    toggleFullscreen: () => set((s) => ({ isFullscreen: !s.isFullscreen })),
    setActiveSession: (id) => {
      if (id) {
        storage.setItem(wsKey(SESSION_STORAGE_KEY), id);
      } else {
        storage.removeItem(wsKey(SESSION_STORAGE_KEY));
      }
      set({ activeSessionId: id });
    },
    setPendingTask: (taskId) => set({ pendingTaskId: taskId, timelineItems: [] }),
    setSelectedAgentId: (id) => {
      storage.setItem(wsKey(AGENT_STORAGE_KEY), id);
      set({ selectedAgentId: id });
    },
    setShowHistory: (show) => set({ showHistory: show }),
    addTimelineItem: (item) =>
      set((s) => {
        if (s.timelineItems.some((t) => t.seq === item.seq)) return s;
        return {
          timelineItems: [...s.timelineItems, item].sort(
            (a, b) => a.seq - b.seq,
          ),
        };
      }),
    clearTimeline: () => set({ timelineItems: [] }),
  }));

  registerForWorkspaceRehydration(() => {
    store.setState({
      activeSessionId: storage.getItem(wsKey(SESSION_STORAGE_KEY)),
      selectedAgentId: storage.getItem(wsKey(AGENT_STORAGE_KEY)),
      timelineItems: [],
    });
  });

  return store;
}
