import { createDraftStore } from "../drafts/create-draft-store";

export interface ChatRunDraft {
  id: string;
  session: string;
  task: string;
  content: string;
}

// Unconfirmed user intent, not a cache of server receipts. Keep every attempt
// until its response is known, even if the composer now holds a newer draft.
export const useChatRunDraftStore = createDraftStore<{ attempts: ChatRunDraft[] }>({
  storageKey: "multica:chat:run-drafts",
  emptyData: { attempts: [] },
  hasMeaningful: ({ attempts }) => attempts.length > 0,
});

export function forgetChatRunDraft(id: string) {
  const store = useChatRunDraftStore.getState();
  if (!store.draft.attempts.some((attempt) => attempt.id === id)) return;
  store.setDraft({ attempts: store.draft.attempts.filter((attempt) => attempt.id !== id) });
}
