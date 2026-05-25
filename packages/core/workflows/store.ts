import { create } from "zustand";

export type EditorMode = "view" | "edit" | "connect";

interface WorkflowEditorState {
  selectedNodeId: string | null;
  selectedEdgeId: string | null;
  mode: EditorMode;
  pendingEdgeSource: string | null;

  selectNode: (id: string | null) => void;
  selectEdge: (id: string | null) => void;
  setMode: (mode: EditorMode) => void;
  setPendingEdgeSource: (id: string | null) => void;
  reset: () => void;
}

const initialState = {
  selectedNodeId: null as string | null,
  selectedEdgeId: null as string | null,
  mode: "view" as EditorMode,
  pendingEdgeSource: null as string | null,
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

  reset: () => set(initialState),
}));