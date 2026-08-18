/**
 * Per-session chat drafts, persisted in AsyncStorage per user + workspace.
 * AsyncStorage is appropriate for the potentially long free-form text and
 * avoids SecureStore's small per-value capacity.
 *
 * Key conventions:
 *   - Real session id (UUID) for any existing session
 *   - DRAFT_NEW_SESSION sentinel for the not-yet-created new-chat input
 */
import AsyncStorage from "@react-native-async-storage/async-storage";
import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { draftStorageKey, readDraftPartition } from "./draft-persistence";

export const DRAFT_NEW_SESSION = "__new__";

interface ChatDraftsState {
  drafts: Record<string, string>;
  setDraft: (sessionId: string, text: string) => void;
  clearDraft: (sessionId: string) => void;
  /** Move the `__new__` draft onto a freshly-created session id without
   *  the user seeing an empty input on the first frame after send. */
  promoteNewDraft: (newSessionId: string) => void;
}

export const useChatDraftsStore = create<ChatDraftsState>()(persist((set, get) => ({
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
}), {
  name: "multica_draft:chat:unscoped",
  storage: createJSONStorage(() => AsyncStorage),
  skipHydration: true,
  partialize: (state) => ({ drafts: state.drafts }),
}));

/** Hydrate this workspace's drafts before rendering its routes. */
export async function hydrateChatDrafts(userId: string, workspaceSlug: string) {
  const name = draftStorageKey("chat", userId, workspaceSlug);
  const persisted = await readDraftPartition<Pick<ChatDraftsState, "drafts">>(
    "chat",
    userId,
    workspaceSlug,
  );
  useChatDraftsStore.persist.setOptions({
    name,
  });
  useChatDraftsStore.setState({ drafts: persisted?.drafts ?? {} });
}
