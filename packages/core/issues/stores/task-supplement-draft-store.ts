import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { registerDraftCleanup } from "../../drafts/cleanup-registry";
import { defaultStorage } from "../../platform/storage";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";

export interface TaskSupplementDraft {
  issueId: string;
  content: string;
  open: boolean;
  ended: boolean;
  clientRequestId?: string;
  updatedAt: number;
}

interface TaskSupplementDraftStore {
  drafts: Record<string, TaskSupplementDraft>;
  open: (taskId: string, issueId: string) => void;
  setContent: (taskId: string, issueId: string, content: string) => void;
  setRequestId: (taskId: string, clientRequestId: string) => void;
  markEnded: (taskId: string) => void;
  clear: (taskId: string) => void;
}

const TTL_MS = 30 * 24 * 60 * 60 * 1000;

function prune(drafts: Record<string, TaskSupplementDraft>): Record<string, TaskSupplementDraft> {
  const cutoff = Date.now() - TTL_MS;
  return Object.fromEntries(Object.entries(drafts).filter(([, draft]) =>
    draft.updatedAt >= cutoff && (!!draft.content.trim() || draft.open),
  ));
}

export const useTaskSupplementDraftStore = create<TaskSupplementDraftStore>()(
  persist(
    (set) => ({
      drafts: {},
      open: (taskId, issueId) => set((state) => ({
        drafts: {
          ...state.drafts,
          [taskId]: {
            issueId,
            content: state.drafts[taskId]?.content ?? "",
            clientRequestId: state.drafts[taskId]?.clientRequestId,
            ended: state.drafts[taskId]?.ended ?? false,
            open: true,
            updatedAt: Date.now(),
          },
        },
      })),
      setContent: (taskId, issueId, content) => set((state) => ({
        drafts: {
          ...state.drafts,
          [taskId]: {
            issueId,
            content,
            open: true,
            ended: state.drafts[taskId]?.ended ?? false,
            // Editing creates a new logical request; retries of unchanged text
            // keep the prior id through setRequestId instead.
            clientRequestId: undefined,
            updatedAt: Date.now(),
          },
        },
      })),
      setRequestId: (taskId, clientRequestId) => set((state) => {
        const draft = state.drafts[taskId];
        if (!draft) return state;
        return { drafts: { ...state.drafts, [taskId]: { ...draft, clientRequestId, updatedAt: Date.now() } } };
      }),
      markEnded: (taskId) => set((state) => {
        const draft = state.drafts[taskId];
        if (!draft) return state;
        if (!draft.content.trim()) {
          const drafts = { ...state.drafts };
          delete drafts[taskId];
          return { drafts };
        }
        if (draft.ended) return state;
        return { drafts: { ...state.drafts, [taskId]: { ...draft, open: true, ended: true, updatedAt: Date.now() } } };
      }),
      clear: (taskId) => set((state) => {
        if (!(taskId in state.drafts)) return state;
        const drafts = { ...state.drafts };
        delete drafts[taskId];
        return { drafts };
      }),
    }),
    {
      name: "multica_task_supplement_drafts",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      merge: (persisted, current) => ({
        ...current,
        drafts: prune((persisted as { drafts?: Record<string, TaskSupplementDraft> } | undefined)?.drafts ?? {}),
      }),
    },
  ),
);

registerForWorkspaceRehydration(() => useTaskSupplementDraftStore.persist.rehydrate());

registerDraftCleanup({
  storageKey: "multica_task_supplement_drafts",
  workspaceScoped: true,
  resetInMemory: () => useTaskSupplementDraftStore.setState({ drafts: {} }),
});
