import { create } from "zustand";
import type { StorageAdapter } from "../types";
import { getCurrentSlug, registerForWorkspaceRehydration } from "../platform/workspace-storage";
import { createLogger } from "../logger";

const logger = createLogger("chat.store");

const AGENT_STORAGE_KEY = "multica:chat:selectedAgentId";
const SESSION_STORAGE_KEY = "multica:chat:activeSessionId";
/** Drafts are stored as one JSON blob per workspace: { [sessionId]: text }. */
const DRAFTS_KEY = "multica:chat:drafts";
/** Placeholder sessionId for a chat that hasn't been created yet. */
export const DRAFT_NEW_SESSION = "__new__";
const CHAT_WIDTH_KEY = "multica:chat:width";
const CHAT_HEIGHT_KEY = "multica:chat:height";
const CHAT_EXPANDED_KEY = "multica:chat:expanded";
/** Focus mode is a personal preference — global across workspaces/sessions. */
const FOCUS_MODE_KEY = "multica:chat:focusMode";

function readDrafts(storage: StorageAdapter, key: string): Record<string, string> {
  const raw = storage.getItem(key);
  if (!raw) return {};
  try {
    const parsed = JSON.parse(raw);
    return typeof parsed === "object" && parsed !== null ? parsed : {};
  } catch {
    return {};
  }
}

function writeDrafts(storage: StorageAdapter, key: string, drafts: Record<string, string>) {
  // Prune empty entries so the blob doesn't grow unbounded.
  const pruned: Record<string, string> = {};
  for (const [k, v] of Object.entries(drafts)) {
    if (v) pruned[k] = v;
  }
  if (Object.keys(pruned).length === 0) {
    storage.removeItem(key);
  } else {
    storage.setItem(key, JSON.stringify(pruned));
  }
}

export const CHAT_MIN_W = 360;
export const CHAT_MIN_H = 480;
export const CHAT_DEFAULT_W = 420;
export const CHAT_DEFAULT_H = 600;

/**
 * Kept as a public type because existing consumers (chat-message-list,
 * views/chat types) import it. Items themselves no longer live in the
 * store — they flow through the React Query cache keyed by task id.
 */
export interface ChatTimelineItem {
  seq: number;
  type: "tool_use" | "tool_result" | "thinking" | "text" | "error";
  tool?: string;
  content?: string;
  input?: Record<string, unknown>;
  output?: string;
}

/**
 * A derived "where I am" pointer — not stored, recomputed each render from
 * the current route + react-query cache. The type is exported because
 * consumers (buildAnchorMarkdown, chip props) share the same shape.
 */
export interface ContextAnchor {
  type: "issue" | "project";
  /** UUID for `issue`, UUID for `project`. */
  id: string;
  /** Human-readable label: issue identifier (MUL-1) or project title. */
  label: string;
  /** Optional secondary text — issue title for issue anchors. */
  subtitle?: string;
}

export interface ChatState {
  isOpen: boolean;
  activeSessionId: string | null;
  selectedAgentId: string | null;
  showHistory: boolean;
  /** Drafts per session: sessionId (or DRAFT_NEW_SESSION) → markdown text. */
  inputDrafts: Record<string, string>;
  /**
   * When on, the chat tracks whatever issue/project/inbox-item the user is
   * looking at and prepends it to outgoing messages. Persisted globally so
   * the preference survives workspace switches and reloads.
   */
  focusMode: boolean;
  /** Raw user-chosen size — no clamp applied. UI layer clamps at render time. */
  chatWidth: number;
  chatHeight: number;
  isExpanded: boolean;
  setOpen: (open: boolean) => void;
  toggle: () => void;
  setActiveSession: (id: string | null) => void;
  setSelectedAgentId: (id: string) => void;
  setShowHistory: (show: boolean) => void;
  /** sessionId accepts a real session UUID or DRAFT_NEW_SESSION. */
  setInputDraft: (sessionId: string, draft: string) => void;
  clearInputDraft: (sessionId: string) => void;
  setFocusMode: (on: boolean) => void;
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
    const slug = getCurrentSlug();
    return slug ? `${base}:${slug}` : base;
  };

  const store = create<ChatState>((set, get) => ({
    isOpen: false,
    activeSessionId: storage.getItem(wsKey(SESSION_STORAGE_KEY)),
    selectedAgentId: storage.getItem(wsKey(AGENT_STORAGE_KEY)),
    showHistory: false,
    inputDrafts: readDrafts(storage, wsKey(DRAFTS_KEY)),
    focusMode: storage.getItem(FOCUS_MODE_KEY) === "true",
    chatWidth: Number(storage.getItem(CHAT_WIDTH_KEY)) || CHAT_DEFAULT_W,
    chatHeight: Number(storage.getItem(CHAT_HEIGHT_KEY)) || CHAT_DEFAULT_H,
    isExpanded: storage.getItem(wsKey(CHAT_EXPANDED_KEY)) === "true",
    setOpen: (open) => {
      logger.debug("setOpen", { from: get().isOpen, to: open });
      set({ isOpen: open });
    },
    toggle: () => {
      const next = !get().isOpen;
      logger.debug("toggle", { to: next });
      set({ isOpen: next });
    },
    setActiveSession: (id) => {
      logger.info("setActiveSession", { from: get().activeSessionId, to: id });
      if (id) {
        storage.setItem(wsKey(SESSION_STORAGE_KEY), id);
      } else {
        storage.removeItem(wsKey(SESSION_STORAGE_KEY));
      }
      set({ activeSessionId: id });
    },
    setSelectedAgentId: (id) => {
      logger.info("setSelectedAgentId", { from: get().selectedAgentId, to: id });
      storage.setItem(wsKey(AGENT_STORAGE_KEY), id);
      set({ selectedAgentId: id });
    },
    setShowHistory: (show) => {
      logger.debug("setShowHistory", { to: show });
      set({ showHistory: show });
    },
    setInputDraft: (sessionId, draft) => {
      // Debug level — onUpdate fires on every keystroke.
      logger.debug("setInputDraft", { sessionId, length: draft.length });
      const next = { ...get().inputDrafts, [sessionId]: draft };
      writeDrafts(storage, wsKey(DRAFTS_KEY), next);
      set({ inputDrafts: next });
    },
    setFocusMode: (on) => {
      logger.info("setFocusMode", { to: on });
      if (on) storage.setItem(FOCUS_MODE_KEY, "true");
      else storage.removeItem(FOCUS_MODE_KEY);
      set({ focusMode: on });
    },
    clearInputDraft: (sessionId) => {
      const current = get().inputDrafts;
      if (!(sessionId in current)) {
        logger.debug("clearInputDraft skipped (no draft)", { sessionId });
        return;
      }
      logger.info("clearInputDraft", { sessionId });
      const next = { ...current };
      delete next[sessionId];
      writeDrafts(storage, wsKey(DRAFTS_KEY), next);
      set({ inputDrafts: next });
    },
    setChatSize: (w, h) => {
      logger.debug("setChatSize", { w, h });
      storage.setItem(CHAT_WIDTH_KEY, String(w));
      storage.setItem(CHAT_HEIGHT_KEY, String(h));
      // Dragging = user chose a manual size → exit expanded mode
      storage.removeItem(wsKey(CHAT_EXPANDED_KEY));
      set({ chatWidth: w, chatHeight: h, isExpanded: false });
    },
    setExpanded: (expanded) => {
      logger.info("setExpanded", { to: expanded });
      if (expanded) {
        storage.setItem(wsKey(CHAT_EXPANDED_KEY), "true");
      } else {
        storage.removeItem(wsKey(CHAT_EXPANDED_KEY));
      }
      set({ isExpanded: expanded });
    },
  }));

  registerForWorkspaceRehydration(() => {
    const nextSession = storage.getItem(wsKey(SESSION_STORAGE_KEY));
    const nextAgent = storage.getItem(wsKey(AGENT_STORAGE_KEY));
    const nextDrafts = readDrafts(storage, wsKey(DRAFTS_KEY));
    logger.info("workspace rehydration", {
      prevSession: store.getState().activeSessionId,
      nextSession,
      prevAgent: store.getState().selectedAgentId,
      nextAgent,
      draftCount: Object.keys(nextDrafts).length,
    });
    store.setState({
      activeSessionId: nextSession,
      selectedAgentId: nextAgent,
      inputDrafts: nextDrafts,
    });
  });

  return store;
}
