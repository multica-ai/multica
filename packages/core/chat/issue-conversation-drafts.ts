import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";
import type { TaskInteractionCommand } from "../api/task-interaction-schema";

interface Draft {
  text: string;
  // Retain the idempotency key across an uncertain HTTP response or reload.
  pending?: TaskInteractionCommand;
  followup?: boolean;
}
interface Drafts {
  drafts: Record<string, Draft>;
  setDraft: (key: string, draft: Draft) => void;
}

export const useIssueConversationDrafts = create<Drafts>()(persist((set) => ({
  drafts: {},
  setDraft: (key, draft) => set((state) => {
    const drafts = { ...state.drafts };
    if (!draft.text && !draft.pending) delete drafts[key];
    else drafts[key] = draft;
    return { drafts };
  }),
}), { name: "multica_issue_conversation_drafts", storage: createJSONStorage(() => defaultStorage) }));
