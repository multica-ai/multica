import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import type { IssueStatus, IssuePriority, IssueAssigneeType } from "../../types";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

interface IssueDraft {
  title: string;
  description: string;
  status: IssueStatus;
  priority: IssuePriority;
  assigneeType?: IssueAssigneeType;
  assigneeId?: string;
  dueDate: string | null;
}

const EMPTY_DRAFT: IssueDraft = {
  title: "",
  description: "",
  status: "todo",
  priority: "none",
  assigneeType: undefined,
  assigneeId: undefined,
  dueDate: null,
};

interface IssueDraftStore {
  draft: IssueDraft;
  // Last assignee picked at submit time. Persisted across drafts so the
  // create-issue modal can prefill the picker with the user's most recent
  // choice instead of always opening with no assignee.
  lastAssigneeType?: IssueAssigneeType;
  lastAssigneeId?: string;
  setDraft: (patch: Partial<IssueDraft>) => void;
  clearDraft: () => void;
  setLastAssignee: (type?: IssueAssigneeType, id?: string) => void;
  hasDraft: () => boolean;
}

export const useIssueDraftStore = create<IssueDraftStore>()(
  persist(
    (set, get) => ({
      draft: { ...EMPTY_DRAFT },
      lastAssigneeType: undefined,
      lastAssigneeId: undefined,
      setDraft: (patch) =>
        set((s) => ({ draft: { ...s.draft, ...patch } })),
      clearDraft: () =>
        set((s) => ({
          draft: {
            ...EMPTY_DRAFT,
            assigneeType: s.lastAssigneeType,
            assigneeId: s.lastAssigneeId,
          },
        })),
      setLastAssignee: (type, id) =>
        set({ lastAssigneeType: type, lastAssigneeId: id }),
      hasDraft: () => {
        const { draft } = get();
        return !!(draft.title || draft.description);
      },
    }),
    {
      name: "multica_issue_draft",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
    },
  ),
);

registerForWorkspaceRehydration(() => useIssueDraftStore.persist.rehydrate());
