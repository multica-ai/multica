/**
 * Per-session chat drafts. In-memory only — drafts survive tab switches and
 * navigation, but are lost on app cold start. v1 doesn't persist (an
 * SecureStore-backed write on every keystroke would be wasteful; if user
 * feedback shows people lose work to backgrounding kills, persist via the
 * usual debounced flush pattern in v2).
 *
 * Key conventions:
 *   - Real session id (UUID) for any existing session
 *   - DRAFT_NEW_SESSION sentinel for the not-yet-created new-chat input
 */
import { create } from "zustand";

export const DRAFT_NEW_SESSION = "__new__";

interface ChatDraftsState {
  drafts: Record<string, string>;
  setDraft: (sessionId: string, text: string) => void;
  clearDraft: (sessionId: string) => void;
  /** Move the `__new__` draft onto a freshly-created session id without
   *  the user seeing an empty input on the first frame after send. */
  promoteNewDraft: (newSessionId: string) => void;
}

export const useChatDraftsStore = create<ChatDraftsState>((set, get) => ({
  drafts: {},
  setDraft: (sessionId, text) => {
    const current = get().drafts;
    // Skip the set when the value is identical — Zustand would still emit
    // a notification and trigger a re-render of every selector subscriber.
    if (current[sessionId] === text) return;
    if (text === "") {
      // Empty input == no draft; prune so we don't accumulate dead keys.
      if (!(sessionId in current)) return;
      const next = { ...current };
      delete next[sessionId];
      set({ drafts: next });
      return;
    }
    set({ drafts: { ...current, [sessionId]: text } });
  },
  clearDraft: (sessionId) => {
    const current = get().drafts;
    if (!(sessionId in current)) return;
    const next = { ...current };
    delete next[sessionId];
    set({ drafts: next });
  },
  promoteNewDraft: (newSessionId) => {
    const current = get().drafts;
    const pending = current[DRAFT_NEW_SESSION];
    if (!pending) return;
    const next = { ...current, [newSessionId]: pending };
    delete next[DRAFT_NEW_SESSION];
    set({ drafts: next });
  },
}));
