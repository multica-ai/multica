import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";
import type { Workflow, WorkflowDraft } from "../types/workflow";

export interface WorkflowDraftEntry {
  draft: WorkflowDraft;
  baseRevision: number;
  generation: number;
}
interface SaveState { saving: boolean; error: Error | null }
interface WorkflowDraftState {
  entries: Record<string, WorkflowDraftEntry>;
  saves: Record<string, SaveState>;
  edit(key: string, server: Workflow, patch: Partial<WorkflowDraft>): void;
  discard(key: string): void;
  saved(key: string, generation: number, revision: number): void;
  setSaveState(key: string, value: SaveState): void;
}
export const workflowDraftKey = (wsId: string, id: string) => `${wsId}:${id}`;
export const workflowToDraft = (workflow: Workflow): WorkflowDraft => ({ name: workflow.name, description: workflow.description, graph: workflow.graph });

export const useWorkflowDraftStore = create<WorkflowDraftState>()(persist((set) => ({
  entries: {}, saves: {},
  edit: (key, server, patch) => set((state) => {
    const entry = state.entries[key];
    const base = entry?.draft ?? workflowToDraft(server);
    const draft = { ...base, ...patch };
    if (JSON.stringify(base) === JSON.stringify(draft)) return state;
    return {
      entries: { ...state.entries, [key]: { draft, baseRevision: entry?.baseRevision ?? server.revision, generation: (entry?.generation ?? 0) + 1 } },
      saves: { ...state.saves, [key]: { saving: state.saves[key]?.saving ?? false, error: null } },
    };
  }),
  discard: (key) => set((state) => {
    const entries = { ...state.entries }; delete entries[key];
    const saves = { ...state.saves }; delete saves[key];
    return { entries, saves };
  }),
  saved: (key, generation, revision) => set((state) => {
    const entry = state.entries[key];
    if (!entry) return state;
    const entries = { ...state.entries };
    if (entry.generation === generation) delete entries[key];
    else entries[key] = { ...entry, baseRevision: revision };
    return { entries };
  }),
  setSaveState: (key, value) => set((state) => ({ saves: { ...state.saves, [key]: value } })),
}), {
  name: "multica-workflow-drafts",
  storage: createJSONStorage(() => defaultStorage),
  partialize: (state) => ({ entries: state.entries }),
  version: 1,
}));

const pendingSaves = new Map<string, Promise<Workflow>>();

// One serialized writer per document, even across tab mounts. Only dirty drafts
// are durable; fetched documents stay in the Query cache.
export async function flushWorkflowDraft(
  key: string,
  server: Workflow,
  write: (draft: WorkflowDraft, revision: number) => Promise<Workflow>,
): Promise<Workflow> {
  const running = pendingSaves.get(key);
  if (running) return running;
  const save = async () => {
    let latest = server;
    const store = useWorkflowDraftStore.getState;
    store().setSaveState(key, { saving: true, error: null });
    try {
      for (;;) {
        const entry = store().entries[key];
        if (!entry) return latest;
        latest = await write(entry.draft, entry.baseRevision);
        store().saved(key, entry.generation, latest.revision);
      }
    } catch (error) {
      const normalized = error instanceof Error ? error : new Error(String(error));
      store().setSaveState(key, { saving: false, error: normalized });
      throw normalized;
    } finally {
      store().setSaveState(key, { saving: false, error: store().saves[key]?.error ?? null });
    }
  };
  const promise = save();
  pendingSaves.set(key, promise);
  try { return await promise; } finally { pendingSaves.delete(key); }
}
