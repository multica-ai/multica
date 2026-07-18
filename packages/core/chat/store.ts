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
/**
 * Ids of durable draft restores (#5219) this client has already written into a
 * composer. Persisted, because the server-side consume that follows can be lost
 * (retries exhausted, the app closed mid-flight) and the row would then be
 * re-offered on the next fetch — re-restoring a prompt the user has since sent.
 * The ledger makes the hand-off at-most-once regardless: an id in here is never
 * offered again, only reconciled (consumed again) until the row is gone.
 */
const APPLIED_RESTORES_KEY = "multica:chat:applied-draft-restores";
/**
 * Local restore requests waiting to reach a composer, queued per session (#5219).
 *
 * These are the restores with NO server copy — a send that failed, or a cancel
 * that answered synchronously. The send already cleared the persisted draft, so
 * this queue is the only place their text exists. It is persisted for exactly
 * that reason: a request the composer cannot act on yet (the user is looking at
 * another session, or has work in progress in this one) must survive an unmount,
 * a refresh, or a close, and be re-offered when they come back.
 *
 * Durable restores (which have a server row) deliberately do NOT go in here —
 * they are refetchable, so dropping one loses nothing.
 */
const PENDING_SEND_RESTORES_KEY = "multica:chat:pending-send-restores";
/**
 * Draft slot for a chat that hasn't been created yet. There is exactly one per
 * workspace: the new-chat composer's identity is "the chat I have not created",
 * not "the chat I have not created with agent X". `selectedAgentId` is the send
 * target, not draft ownership, so switching agent mid-compose keeps the text
 * (MUL-4864). Created sessions keep their own slot, keyed by session id.
 */
export const DRAFT_NEW_SESSION = "__new__";

/** Pre-MUL-4864 per-agent new-chat slots, shaped `__new__:<agentId>`. */
const LEGACY_NEW_SESSION_PREFIX = `${DRAFT_NEW_SESSION}:`;
const CHAT_WIDTH_KEY = "multica:chat:width";
const CHAT_HEIGHT_KEY = "multica:chat:height";
const CHAT_EXPANDED_KEY = "multica:chat:expanded";
/**
 * Open/closed preference, persisted globally (not per-workspace) — most users
 * have one habitual chat-panel preference across workspaces. Missing key =
 * new user (or cleared storage); default to CLOSED so opening a workspace
 * never pops the chat window uninvited (the FAB keeps it discoverable).
 * Once the user toggles even once, their explicit choice is respected on
 * every subsequent reload.
 */
const OPEN_KEY = "multica:chat:isOpen";
/**
 * Whether the floating chat window (FAB + overlay) is available at all,
 * persisted globally like OPEN_KEY. This is the Settings → Chat preference:
 * when off, the FAB/overlay never mount and Chat lives only in its tab.
 * Missing key = default ON — the floating window is on by default and can
 * be turned off from the Settings → Chat tab.
 */
const FLOATING_KEY = "multica:chat:floatingChatEnabled";

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

function readAppliedRestores(storage: StorageAdapter, key: string): string[] {
  const raw = storage.getItem(key);
  if (!raw) return [];
  try {
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed.filter((id): id is string => typeof id === "string");
  } catch {
    return [];
  }
}

function writeAppliedRestores(storage: StorageAdapter, key: string, ids: string[]) {
  if (ids.length === 0) storage.removeItem(key);
  else storage.setItem(key, JSON.stringify(ids));
}

function isPendingSendRestore(value: unknown): value is PendingSendRestore {
  if (typeof value !== "object" || value === null) return false;
  const v = value as { id?: unknown; content?: unknown; sessionId?: unknown };
  return (
    typeof v.id === "string" && typeof v.content === "string" && typeof v.sessionId === "string"
  );
}

function readPendingSendRestores(
  storage: StorageAdapter,
  key: string,
): Record<string, PendingSendRestore[]> {
  const raw = storage.getItem(key);
  if (!raw) return {};
  try {
    const parsed = JSON.parse(raw);
    if (typeof parsed !== "object" || parsed === null) return {};
    const out: Record<string, PendingSendRestore[]> = {};
    for (const [sessionId, value] of Object.entries(parsed)) {
      if (!Array.isArray(value)) continue;
      const queued = value.filter(isPendingSendRestore).map((r) => ({
        ...r,
        attachments: Array.isArray(r.attachments) ? r.attachments.filter(isAttachmentDraft) : [],
      }));
      if (queued.length > 0) out[sessionId] = queued;
    }
    return out;
  } catch {
    return {};
  }
}

function writePendingSendRestores(
  storage: StorageAdapter,
  key: string,
  queues: Record<string, PendingSendRestore[]>,
) {
  const pruned: Record<string, PendingSendRestore[]> = {};
  for (const [k, v] of Object.entries(queues)) {
    if (v.length > 0) pruned[k] = v;
  }
  if (Object.keys(pruned).length === 0) storage.removeItem(key);
  else storage.setItem(key, JSON.stringify(pruned));
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

/**
 * Fold the legacy per-agent new-chat slots into the single DRAFT_NEW_SESSION
 * slot, then drop them.
 *
 * The legacy slots carry no timestamp, so when several exist there is no way to
 * tell which one the user typed last — and "keep them all" has nowhere to put
 * the losers now that there is one composer. Adopt the slot belonging to the
 * persisted `selectedAgentId` (the draft this workspace would have shown on
 * open, so the only one the user can be expecting) and discard the rest: those
 * extra slots ARE the invisible multi-draft state this change removes.
 *
 * Both write paths prune empty values, so key presence means content.
 * Idempotent: once the legacy keys are gone this is an allocation-free no-op.
 */
function migrateLegacyNewChatSlots<T>(
  slots: Record<string, T>,
  selectedAgentId: string | null,
): { slots: Record<string, T>; changed: boolean } {
  const legacyKeys = Object.keys(slots).filter((k) => k.startsWith(LEGACY_NEW_SESSION_PREFIX));
  if (legacyKeys.length === 0) return { slots, changed: false };
  const next = { ...slots };
  const adopted = next[`${LEGACY_NEW_SESSION_PREFIX}${selectedAgentId ?? ""}`];
  // Never clobber the unified slot: whatever is in it was written under the
  // current scheme and is therefore newer than any legacy leftover.
  if (!(DRAFT_NEW_SESSION in next) && adopted !== undefined) {
    next[DRAFT_NEW_SESSION] = adopted;
  }
  for (const key of legacyKeys) delete next[key];
  logger.info("migrating legacy per-agent new-chat drafts", {
    legacyCount: legacyKeys.length,
    selectedAgentId,
    adopted: DRAFT_NEW_SESSION in next,
  });
  return { slots: next, changed: true };
}

/**
 * Read both draft maps and migrate them together, against the same
 * `selectedAgentId` — text and attachments must never disagree on which legacy
 * new-chat draft survived, or the user gets agent A's words with agent B's
 * files.
 */
function loadDraftSlots(
  storage: StorageAdapter,
  draftsKey: string,
  attachmentsKey: string,
  selectedAgentId: string | null,
): { inputDrafts: Record<string, string>; inputDraftAttachments: Record<string, Attachment[]> } {
  const drafts = migrateLegacyNewChatSlots(readDrafts(storage, draftsKey), selectedAgentId);
  const attachments = migrateLegacyNewChatSlots(
    readDraftAttachments(storage, attachmentsKey),
    selectedAgentId,
  );
  if (drafts.changed) writeDrafts(storage, draftsKey, drafts.slots);
  if (attachments.changed) writeDraftAttachments(storage, attachmentsKey, attachments.slots);
  return { inputDrafts: drafts.slots, inputDraftAttachments: attachments.slots };
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

/**
 * A restore with no server copy, waiting for a composer that can take it. Its
 * text exists nowhere else, so it lives in persisted storage until it is applied
 * (see PENDING_SEND_RESTORES_KEY).
 */
export interface PendingSendRestore {
  id: string;
  content: string;
  attachments?: Attachment[];
  /** The session whose composer this belongs to. Never empty. */
  sessionId: string;
}

export interface ChatState {
  isOpen: boolean;
  /** Settings preference: is the floating chat window available at all. */
  floatingChatEnabled: boolean;
  activeSessionId: string | null;
  selectedAgentId: string | null;
  /** Drafts per session: sessionId (or DRAFT_NEW_SESSION) → markdown text. */
  inputDrafts: Record<string, string>;
  /** Attachment rows referenced by each input draft. */
  inputDraftAttachments: Record<string, Attachment[]>;
  /** Durable draft restores already written into a composer (#5219). */
  appliedDraftRestoreIds: string[];
  /** Server-less restores waiting for their session's composer, per session (#5219). */
  pendingSendRestores: Record<string, PendingSendRestore[]>;
  /** Raw user-chosen size — no clamp applied. UI layer clamps at render time. */
  chatWidth: number;
  chatHeight: number;
  isExpanded: boolean;
  setOpen: (open: boolean) => void;
  toggle: () => void;
  setFloatingChatEnabled: (enabled: boolean) => void;
  setActiveSession: (id: string | null) => void;
  setSelectedAgentId: (id: string) => void;
  /** sessionId accepts a real session UUID or DRAFT_NEW_SESSION. */
  setInputDraft: (sessionId: string, draft: string) => void;
  setInputDraftAttachments: (sessionId: string, attachments: Attachment[]) => void;
  addInputDraftAttachment: (sessionId: string, attachment: Attachment) => void;
  clearInputDraft: (sessionId: string) => void;
  /** Record that a durable restore reached the composer; survives a reload. */
  markDraftRestoreApplied: (restoreId: string) => void;
  /** Drop the ledger entry once the server row is confirmed gone. */
  forgetDraftRestoreApplied: (restoreId: string) => void;
  /** Queue a server-less restore for its session; survives unmount/refresh. */
  enqueuePendingSendRestore: (restore: PendingSendRestore) => void;
  /** Drop a queued restore once its text is safely in the (persisted) draft. */
  dequeuePendingSendRestore: (sessionId: string, restoreId: string) => void;
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
  // "true" / "false") keeps the "new user → closed" default while still
  // honouring an explicit "I opened it" choice on every reload.
  const storedOpen = storage.getItem(OPEN_KEY);
  const initialIsOpen = storedOpen === "true";

  // Default ON: the floating window is enabled unless the user explicitly
  // turned it off ("false") from the Settings → Chat tab. A missing key
  // (new user) resolves to enabled.
  const initialFloatingEnabled = storage.getItem(FLOATING_KEY) !== "false";

  const initialAgentId = storage.getItem(wsKey(AGENT_STORAGE_KEY));
  const initialDraftSlots = loadDraftSlots(
    storage,
    wsKey(DRAFTS_KEY),
    wsKey(DRAFT_ATTACHMENTS_KEY),
    initialAgentId,
  );

  const store = create<ChatState>((set, get) => ({
    isOpen: initialIsOpen,
    floatingChatEnabled: initialFloatingEnabled,
    activeSessionId: storage.getItem(wsKey(SESSION_STORAGE_KEY)),
    selectedAgentId: initialAgentId,
    inputDrafts: initialDraftSlots.inputDrafts,
    inputDraftAttachments: initialDraftSlots.inputDraftAttachments,
    appliedDraftRestoreIds: readAppliedRestores(storage, wsKey(APPLIED_RESTORES_KEY)),
    pendingSendRestores: readPendingSendRestores(storage, wsKey(PENDING_SEND_RESTORES_KEY)),
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
    setFloatingChatEnabled: (enabled) => {
      logger.info("setFloatingChatEnabled", { to: enabled });
      storage.setItem(FLOATING_KEY, String(enabled));
      // Turning the feature off should also collapse an open overlay so it
      // does not linger until the next toggle.
      set(enabled ? { floatingChatEnabled: true } : { floatingChatEnabled: false, isOpen: false });
      if (!enabled) storage.setItem(OPEN_KEY, "false");
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
    // Append-only until the server confirms. There is deliberately no capacity
    // cap: every entry in here is an UNconfirmed consume, and evicting one
    // silently re-arms the restore it was suppressing — the row is still on the
    // server, so the next fetch would offer an already-applied prompt again and
    // the user could send it twice. Entries leave only through
    // forgetDraftRestoreApplied, i.e. only once the row is provably gone, which
    // bounds the ledger by the number of restores whose consume is still failing.
    markDraftRestoreApplied: (restoreId) => {
      const current = get().appliedDraftRestoreIds;
      if (current.includes(restoreId)) return;
      const next = [...current, restoreId];
      writeAppliedRestores(storage, wsKey(APPLIED_RESTORES_KEY), next);
      set({ appliedDraftRestoreIds: next });
    },
    /** Called only on a confirmed consume: the server row is gone. */
    forgetDraftRestoreApplied: (restoreId) => {
      const current = get().appliedDraftRestoreIds;
      if (!current.includes(restoreId)) return;
      const next = current.filter((id) => id !== restoreId);
      writeAppliedRestores(storage, wsKey(APPLIED_RESTORES_KEY), next);
      set({ appliedDraftRestoreIds: next });
    },
    // Queued per session, not in one shared slot: a request for a session the
    // user is not looking at must never hold the composer's only restore slot
    // (it would starve the session they ARE looking at), and it has no server
    // copy to fall back on, so it cannot simply be dropped either. FIFO, so two
    // failures against the same session are both recovered, oldest first.
    enqueuePendingSendRestore: (restore) => {
      if (!restore.sessionId || !restore.id) return;
      const current = get().pendingSendRestores;
      const existing = current[restore.sessionId] ?? [];
      if (existing.some((r) => r.id === restore.id)) return;
      logger.info("enqueuePendingSendRestore", {
        sessionId: restore.sessionId,
        restoreId: restore.id,
      });
      const next = { ...current, [restore.sessionId]: [...existing, restore] };
      writePendingSendRestores(storage, wsKey(PENDING_SEND_RESTORES_KEY), next);
      set({ pendingSendRestores: next });
    },
    /** Only after the text has landed in the draft, which is itself persisted. */
    dequeuePendingSendRestore: (sessionId, restoreId) => {
      const current = get().pendingSendRestores;
      const existing = current[sessionId];
      if (!existing?.some((r) => r.id === restoreId)) return;
      logger.info("dequeuePendingSendRestore", { sessionId, restoreId });
      const remaining = existing.filter((r) => r.id !== restoreId);
      const next = { ...current };
      if (remaining.length > 0) next[sessionId] = remaining;
      else delete next[sessionId];
      writePendingSendRestores(storage, wsKey(PENDING_SEND_RESTORES_KEY), next);
      set({ pendingSendRestores: next });
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
    // Drafts are namespaced per workspace, so the workspace being switched TO
    // has its own legacy slots to fold — migrate against that workspace's own
    // persisted agent, not the one we are leaving.
    const { inputDrafts: nextDrafts, inputDraftAttachments: nextDraftAttachments } = loadDraftSlots(
      storage,
      wsKey(DRAFTS_KEY),
      wsKey(DRAFT_ATTACHMENTS_KEY),
      nextAgent,
    );
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
      appliedDraftRestoreIds: readAppliedRestores(storage, wsKey(APPLIED_RESTORES_KEY)),
      pendingSendRestores: readPendingSendRestores(storage, wsKey(PENDING_SEND_RESTORES_KEY)),
    });
  });

  return store;
}
