import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import type { IssueStatus, IssuePriority, IssueAssigneeType, Attachment } from "../../types";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

interface IssueDraft {
  title: string;
  description: string;
  status: IssueStatus;
  priority: IssuePriority;
  assigneeType?: IssueAssigneeType;
  assigneeId?: string;
  startDate: string | null;
  dueDate: string | null;
  /** Label IDs chosen in the create dialog. Attached to the issue right
   *  after it is created (the create endpoint takes no labels), so they are
   *  kept as a plain id list rather than full Label objects. */
  labelIds: string[];
  attachments: Attachment[];
}

const EMPTY_DRAFT: IssueDraft = {
  title: "",
  description: "",
  status: "todo",
  priority: "none",
  assigneeType: undefined,
  assigneeId: undefined,
  startDate: null,
  dueDate: null,
  labelIds: [],
  attachments: [],
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
      // Drafts persisted by older builds predate fields added later (e.g.
      // `attachments`). Backfill EMPTY_DRAFT defaults on rehydrate so every
      // read site can rely on the declared IssueDraft shape instead of
      // re-defending with `?? fallback`.
      merge: (persistedState, currentState) => {
        const persisted = (persistedState ?? {}) as Partial<IssueDraftStore>;
        return {
          ...currentState,
          ...persisted,
          draft: { ...EMPTY_DRAFT, ...persisted.draft },
        };
      },
    },
  ),
);

registerForWorkspaceRehydration(() => useIssueDraftStore.persist.rehydrate());
