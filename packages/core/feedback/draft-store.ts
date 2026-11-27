import { createDraftStore } from "../drafts/create-draft-store";

interface FeedbackDraft {
  message: string;
}

export const useFeedbackDraftStore = createDraftStore<FeedbackDraft>({
  storageKey: "multica_feedback_draft",
  emptyData: { message: "" },
  hasMeaningful: (d) => !!d.message,
});
