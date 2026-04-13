import { create } from "zustand";
import type { StorageAdapter } from "../types";
import { getCurrentWorkspaceId, registerForWorkspaceRehydration } from "../platform/workspace-storage";

const AGENT_STORAGE_KEY = "multica:chat:selectedAgentId";
const SESSION_STORAGE_KEY = "multica:chat:activeSessionId";
const DRAFT_KEY = "multica:chat:draft";
const CHAT_WIDTH_KEY = "multica:chat:width";
const CHAT_HEIGHT_KEY = "multica:chat:height";
const CHAT_EXPANDED_KEY = "multica:chat:expanded";

export const CHAT_MIN_W = 360;
export const CHAT_MIN_H = 480;
export const CHAT_DEFAULT_W = 420;
export const CHAT_DEFAULT_H = 600;

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
  activeSessionId: string | null;
  pendingTaskId: string | null;
  selectedAgentId: string | null;
  showHistory: boolean;
  timelineItems: ChatTimelineItem[];
  inputDraft: string;
  /** Raw user-chosen size — no clamp applied. UI layer clamps at render time. */
  chatWidth: number;
  chatHeight: number;
  isExpanded: boolean;
  setOpen: (open: boolean) => void;
  toggle: () => void;
  setActiveSession: (id: string | null) => void;
  setPendingTask: (taskId: string | null) => void;
  setSelectedAgentId: (id: string) => void;
  setShowHistory: (show: boolean) => void;
  addTimelineItem: (item: ChatTimelineItem) => void;
  clearTimeline: () => void;
  setInputDraft: (draft: string) => void;
  clearInputDraft: () => void;
  /** Persist raw size and auto-exit expanded mode. */
  setChatSize: (width: number, height: number) => void;
  setExpanded: (expanded: boolean) => void;
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
    activeSessionId: storage.getItem(wsKey(SESSION_STORAGE_KEY)),
    pendingTaskId: null,
    selectedAgentId: storage.getItem(wsKey(AGENT_STORAGE_KEY)),
    showHistory: false,
    timelineItems: [],
    inputDraft: storage.getItem(wsKey(DRAFT_KEY)) ?? "",
    chatWidth: Number(storage.getItem(CHAT_WIDTH_KEY)) || CHAT_DEFAULT_W,
    chatHeight: Number(storage.getItem(CHAT_HEIGHT_KEY)) || CHAT_DEFAULT_H,
    isExpanded: storage.getItem(wsKey(CHAT_EXPANDED_KEY)) === "true",
    setOpen: (open) => set({ isOpen: open }),
    toggle: () => set((s) => ({ isOpen: !s.isOpen })),
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
    setInputDraft: (draft) => {
      if (draft) {
        storage.setItem(wsKey(DRAFT_KEY), draft);
      } else {
        storage.removeItem(wsKey(DRAFT_KEY));
      }
      set({ inputDraft: draft });
    },
    clearInputDraft: () => {
      storage.removeItem(wsKey(DRAFT_KEY));
      set({ inputDraft: "" });
    },
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
    setChatSize: (w, h) => {
      storage.setItem(CHAT_WIDTH_KEY, String(w));
      storage.setItem(CHAT_HEIGHT_KEY, String(h));
      // Dragging = user chose a manual size → exit expanded mode
      storage.removeItem(wsKey(CHAT_EXPANDED_KEY));
      set({ chatWidth: w, chatHeight: h, isExpanded: false });
    },
    setExpanded: (expanded) => {
      if (expanded) {
        storage.setItem(wsKey(CHAT_EXPANDED_KEY), "true");
      } else {
        storage.removeItem(wsKey(CHAT_EXPANDED_KEY));
      }
      set({ isExpanded: expanded });
    },
  }));

  registerForWorkspaceRehydration(() => {
    store.setState({
      activeSessionId: storage.getItem(wsKey(SESSION_STORAGE_KEY)),
      selectedAgentId: storage.getItem(wsKey(AGENT_STORAGE_KEY)),
      inputDraft: storage.getItem(wsKey(DRAFT_KEY)) ?? "",
      timelineItems: [],
    });
  });

  return store;
}
