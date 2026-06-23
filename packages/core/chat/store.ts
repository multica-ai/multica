import { create } from "zustand";
import type { StorageAdapter } from "../types";
import type { Attachment } from "../types/attachment";
import { getCurrentSlug, registerForWorkspaceRehydration } from "../platform/workspace-storage";
import { createLogger } from "../logger";

const logger = createLogger("chat.store");

const AGENT_STORAGE_KEY = "multica:chat:selectedAgentId";
const SESSION_STORAGE_KEY = "multica:chat:activeSessionId";
/** Drafts are stored as one JSON blob per workspace: { [sessionId]: text }. */
const DRAFTS_KEY = "multica:chat:drafts";
/** Draft attachment records per workspace: { [sessionId]: Attachment[] }. */
const DRAFT_ATTACHMENTS_KEY = "multica:chat:draft-attachments";
/** Placeholder sessionId for a chat that hasn't been created yet. */
export const DRAFT_NEW_SESSION = "__new__";

/**
 * Draft storage key for an as-yet-uncreated chat with the given agent.
 * Shared by ChatInput (which writes the draft) and ensureSession (which
 * migrates it onto the real session id the moment the session is created),
 * so the two never disagree on the slot name.
 */
export function newSessionDraftKey(selectedAgentId: string | null): string {
  return `${DRAFT_NEW_SESSION}:${selectedAgentId ?? ""}`;
}
const CHAT_WIDTH_KEY = "multica:chat:width";
const CHAT_HEIGHT_KEY = "multica:chat:height";
const CHAT_EXPANDED_KEY = "multica:chat:expanded";
/**
 * Open/closed preference, persisted globally (not per-workspace) — most users
 * have one habitual chat-panel preference across workspaces. Missing key =
 * new user (or cleared storage); default to OPEN so the chat is discoverable.
 * Once the user toggles even once, their explicit choice is respected on
 * every subsequent reload.
 */
const OPEN_KEY = "multica:chat:isOpen";

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

function isAttachmentDraft(value: unknown): value is Attachment {
  return (
    typeof value === "object" &&
    value !== null &&
    typeof (value as { id?: unknown }).id === "string" &&
    typeof (value as { filename?: unknown }).filename === "string"
  );
}

function readDraftAttachments(storage: StorageAdapter, key: string): Record<string, Attachment[]> {
  const raw = storage.getItem(key);
  if (!raw) return {};
  try {
    const parsed = JSON.parse(raw);
    if (typeof parsed !== "object" || parsed === null) return {};
    const out: Record<string, Attachment[]> = {};
    for (const [draftKey, value] of Object.entries(parsed)) {
      if (!Array.isArray(value)) continue;
      const attachments = value.filter(isAttachmentDraft);
      if (attachments.length > 0) out[draftKey] = attachments;
    }
    return out;
  } catch {
    return {};
  }
}

function writeDraftAttachments(
  storage: StorageAdapter,
  key: string,
  drafts: Record<string, Attachment[]>,
) {
  const pruned: Record<string, Attachment[]> = {};
  for (const [k, v] of Object.entries(drafts)) {
    if (v.length > 0) pruned[k] = v;
  }
  if (Object.keys(pruned).length === 0) {
    storage.removeItem(key);
  } else {
    storage.setItem(key, JSON.stringify(pruned));
  }
}

export const CHAT_MIN_W = 360;
export const CHAT_MIN_H = 480;
export const CHAT_DEFAULT_W = 380;
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
  created_at?: string;
}

export interface ChatState {
  isOpen: boolean;
  activeSessionId: string | null;
  selectedAgentId: string | null;
  /** Drafts per session: sessionId (or DRAFT_NEW_SESSION) → markdown text. */
  inputDrafts: Record<string, string>;
  /** Attachment rows referenced by each input draft. */
  inputDraftAttachments: Record<string, Attachment[]>;
  /** Raw user-chosen size — no clamp applied. UI layer clamps at render time. */
  chatWidth: number;
  chatHeight: number;
  isExpanded: boolean;
  setOpen: (open: boolean) => void;
  toggle: () => void;
  setActiveSession: (id: string | null) => void;
  setSelectedAgentId: (id: string) => void;
  /** sessionId accepts a real session UUID or DRAFT_NEW_SESSION. */
  setInputDraft: (sessionId: string, draft: string) => void;
  setInputDraftAttachments: (sessionId: string, attachments: Attachment[]) => void;
  addInputDraftAttachment: (sessionId: string, attachment: Attachment) => void;
  clearInputDraft: (sessionId: string) => void;
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

  // Resolve initial isOpen from storage. The three-state read (null /
  // "true" / "false") is what enables the "new user → open" default while
  // still honouring an explicit "I closed it" choice on every reload.
  const storedOpen = storage.getItem(OPEN_KEY);
  const initialIsOpen = storedOpen === null ? true : storedOpen === "true";

  const store = create<ChatState>((set, get) => ({
    isOpen: initialIsOpen,
    activeSessionId: storage.getItem(wsKey(SESSION_STORAGE_KEY)),
    selectedAgentId: storage.getItem(wsKey(AGENT_STORAGE_KEY)),
    inputDrafts: readDrafts(storage, wsKey(DRAFTS_KEY)),
    inputDraftAttachments: readDraftAttachments(storage, wsKey(DRAFT_ATTACHMENTS_KEY)),
    chatWidth: Number(storage.getItem(CHAT_WIDTH_KEY)) || CHAT_DEFAULT_W,
    chatHeight: Number(storage.getItem(CHAT_HEIGHT_KEY)) || CHAT_DEFAULT_H,
    isExpanded: storage.getItem(wsKey(CHAT_EXPANDED_KEY)) === "true",
    setOpen: (open) => {
      logger.debug("setOpen", { from: get().isOpen, to: open });
      storage.setItem(OPEN_KEY, String(open));
      set({ isOpen: open });
    },
    toggle: () => {
      const next = !get().isOpen;
      logger.debug("toggle", { to: next });
      storage.setItem(OPEN_KEY, String(next));
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
    setInputDraft: (sessionId, draft) => {
      // Debug level — onUpdate fires on every keystroke.
      logger.debug("setInputDraft", { sessionId, length: draft.length });
      const next = { ...get().inputDrafts, [sessionId]: draft };
      writeDrafts(storage, wsKey(DRAFTS_KEY), next);
      set({ inputDrafts: next });
    },
    setInputDraftAttachments: (sessionId, attachments) => {
      logger.debug("setInputDraftAttachments", { sessionId, count: attachments.length });
      const next = { ...get().inputDraftAttachments };
      if (attachments.length > 0) next[sessionId] = attachments;
      else delete next[sessionId];
      writeDraftAttachments(storage, wsKey(DRAFT_ATTACHMENTS_KEY), next);
      set({ inputDraftAttachments: next });
    },
    addInputDraftAttachment: (sessionId, attachment) => {
      if (!attachment.id) return;
      const current = get().inputDraftAttachments;
      const existing = current[sessionId] ?? [];
      const nextForKey = existing.some((a) => a.id === attachment.id)
        ? existing.map((a) => (a.id === attachment.id ? attachment : a))
        : [...existing, attachment];
      const next = { ...current, [sessionId]: nextForKey };
      writeDraftAttachments(storage, wsKey(DRAFT_ATTACHMENTS_KEY), next);
      set({ inputDraftAttachments: next });
    },
    clearInputDraft: (sessionId) => {
      const currentDrafts = get().inputDrafts;
      const currentAttachments = get().inputDraftAttachments;
      if (!(sessionId in currentDrafts) && !(sessionId in currentAttachments)) {
        logger.debug("clearInputDraft skipped (no draft)", { sessionId });
        return;
      }
      logger.info("clearInputDraft", { sessionId });
      const nextDrafts = { ...currentDrafts };
      const nextAttachments = { ...currentAttachments };
      delete nextDrafts[sessionId];
      delete nextAttachments[sessionId];
      writeDrafts(storage, wsKey(DRAFTS_KEY), nextDrafts);
      writeDraftAttachments(storage, wsKey(DRAFT_ATTACHMENTS_KEY), nextAttachments);
      set({ inputDrafts: nextDrafts, inputDraftAttachments: nextAttachments });
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
    const nextDraftAttachments = readDraftAttachments(storage, wsKey(DRAFT_ATTACHMENTS_KEY));
    logger.info("workspace rehydration", {
      prevSession: store.getState().activeSessionId,
      nextSession,
      prevAgent: store.getState().selectedAgentId,
      nextAgent,
      draftCount: Object.keys(nextDrafts).length,
      draftAttachmentCount: Object.keys(nextDraftAttachments).length,
    });
    store.setState({
      activeSessionId: nextSession,
      selectedAgentId: nextAgent,
      inputDrafts: nextDrafts,
      inputDraftAttachments: nextDraftAttachments,
    });
  });

  return store;
}
