import { create } from "zustand";
import type { UpdateNodeRequest } from "../types";

export type EditorMode = "view" | "edit" | "connect";

interface NodeEditCache {
  [nodeId: string]: Partial<UpdateNodeRequest>;
}

interface WorkflowEditorState {
  selectedNodeId: string | null;
  selectedEdgeId: string | null;
  mode: EditorMode;
  pendingEdgeSource: string | null;
  nodeEdits: NodeEditCache;

  selectNode: (id: string | null) => void;
  selectEdge: (id: string | null) => void;
  setMode: (mode: EditorMode) => void;
  setPendingEdgeSource: (id: string | null) => void;
  cacheNodeEdits: (nodeId: string, edits: Partial<UpdateNodeRequest>) => void;
  clearNodeEdits: (nodeId: string) => void;
  reset: () => void;
}

const initialState = {
  selectedNodeId: null as string | null,
  selectedEdgeId: null as string | null,
  mode: "view" as EditorMode,
  pendingEdgeSource: null as string | null,
  nodeEdits: {} as NodeEditCache,
};

export const useWorkflowEditorStore = create<WorkflowEditorState>((set) => ({
  ...initialState,

  selectNode: (id) =>
    set({
      selectedNodeId: id,
      selectedEdgeId: null,
    }),

  selectEdge: (id) =>
    set({
      selectedEdgeId: id,
      selectedNodeId: null,
    }),

  setMode: (mode) =>
    set({
      mode,
      pendingEdgeSource: mode === "connect" ? undefined : null,
    }),

  setPendingEdgeSource: (id) => set({ pendingEdgeSource: id }),

  cacheNodeEdits: (nodeId, edits) =>
    set((state) => ({
      nodeEdits: { ...state.nodeEdits, [nodeId]: { ...state.nodeEdits[nodeId], ...edits } },
    })),

  clearNodeEdits: (nodeId) =>
    set((state) => {
      const next = { ...state.nodeEdits };
      delete next[nodeId];
      return { nodeEdits: next };
    }),

  reset: () => set(initialState),
}));
