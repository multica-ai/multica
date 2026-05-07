import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../platform/workspace-storage";
import { defaultStorage } from "../platform/storage";

interface FeedbackDraft {
  message: string;
}

const EMPTY_DRAFT: FeedbackDraft = {
  message: "",
};

interface FeedbackDraftStore {
  draft: FeedbackDraft;
  setDraft: (patch: Partial<FeedbackDraft>) => void;
  clearDraft: () => void;
  hasDraft: () => boolean;
}

export const useFeedbackDraftStore = create<FeedbackDraftStore>()(
  persist(
    (set, get) => ({
      draft: { ...EMPTY_DRAFT },
      setDraft: (patch) =>
        set((s) => ({ draft: { ...s.draft, ...patch } })),
      clearDraft: () =>
        set({ draft: { ...EMPTY_DRAFT } }),
      hasDraft: () => {
        const { draft } = get();
        return !!draft.message;
      },
    }),
    {
      name: "multica_feedback_draft",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
    },
  ),
);

registerForWorkspaceRehydration(() => useFeedbackDraftStore.persist.rehydrate());
