import { createDraftStore } from "../drafts";
export interface DocDraft {
  title: string;
  content: string;
  baseRevision: number;
  baseTitle: string;
  baseContent: string;
  backup?: { title: string; content: string };
}
export const useDocDraftStore = createDraftStore<{
  entries: Record<string, DocDraft>;
}>({
  storageKey: "doc-drafts",
  emptyData: { entries: {} },
  hasMeaningful: (d) => Object.keys(d.entries).length > 0,
});
export function setDocDraft(id: string, draft: DocDraft) {
  const state = useDocDraftStore.getState();
  state.setDraft({ entries: { ...state.draft.entries, [id]: draft } });
}
export function removeDocDraft(id: string) {
  const state = useDocDraftStore.getState();
  const entries = { ...state.draft.entries };
  delete entries[id];
  state.setDraft({ entries });
}
