import { defaultStorage } from "../platform/storage";
import { getCurrentSlug } from "../platform/workspace-storage";
import { createDraftStore } from "../drafts/create-draft-store";

export interface ChatRunDraft {
  id: string;
  session: string;
  task: string;
  content: string;
}

const STORAGE_KEY = "multica:chat:run-drafts";

// Unconfirmed user intent, not a cache of server receipts. Keep every attempt
// until its response is known, even if the composer now holds a newer draft.
export const useChatRunDraftStore = createDraftStore<{ attempts: ChatRunDraft[] }>({
  storageKey: STORAGE_KEY,
  emptyData: { attempts: [] },
  hasMeaningful: ({ attempts }) => attempts.length > 0,
});

export function forgetChatRunDraft(id: string, workspaceSlug = getCurrentSlug()) {
  if (workspaceSlug) {
    const key = `${STORAGE_KEY}:${workspaceSlug}`;
    const raw = defaultStorage.getItem(key);
    // Cleanup may have removed this workspace while the request was in flight.
    // Only subtract from the latest persisted state; never restore a snapshot.
    if (!raw) return;
    let persisted;
    try {
      persisted = JSON.parse(raw);
    } catch {
      return;
    }
    const attempts: ChatRunDraft[] | undefined = persisted?.state?.draft?.attempts;
    if (!Array.isArray(attempts) || !attempts.some((attempt) => attempt.id === id)) return;
    persisted.state.draft.attempts = attempts.filter((attempt) => attempt.id !== id);
    defaultStorage.setItem(key, JSON.stringify(persisted));
  }
  if (workspaceSlug !== getCurrentSlug()) return;
  const store = useChatRunDraftStore.getState();
  if (!store.draft.attempts.some((attempt) => attempt.id === id)) return;
  store.setDraft({ attempts: store.draft.attempts.filter((attempt) => attempt.id !== id) });
}
