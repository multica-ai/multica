import { create } from "zustand";
import type { UpdateNodeRequest } from "../types";

export type EditorMode = "view" | "edit" | "connect";

interface NodeEditCache {
  [nodeId: string]: Partial<UpdateNodeRequest>;
}

interface WorkflowSnapshot {
  nodeEdits: NodeEditCache;
  deletedNodeIds: string[];
}

export interface TrackedAction {
  type: "create-node" | "create-edge" | "delete-edge";
  nodeId?: string;
  edgeId?: string;
  sourceNodeId?: string;
  targetNodeId?: string;
}

interface UndoEntry {
  snapshot: WorkflowSnapshot;
  action?: TrackedAction;
}

const MAX_UNDO_STACK = 50;
const SNAPSHOT_INTERVAL = 600;

interface WorkflowEditorState {
  selectedNodeId: string | null;
  selectedNodeIds: string[];
  selectedEdgeId: string | null;
  mode: EditorMode;
  pendingEdgeSource: string | null;
  nodeEdits: NodeEditCache;
  deletedNodeIds: string[];
  undoStack: UndoEntry[];
  redoStack: UndoEntry[];
  _reverseAction: TrackedAction | null;
  _undoLastTime: number;
  showAnnotations: boolean;
  canvasColorMode: "system" | "light" | "dark";

  selectNode: (id: string | null) => void;
  setSelectedNodeIds: (ids: string[]) => void;
  selectEdge: (id: string | null) => void;
  setMode: (mode: EditorMode) => void;
  setPendingEdgeSource: (id: string | null) => void;
  cacheNodeEdits: (nodeId: string, edits: Partial<UpdateNodeRequest>) => void;
  clearNodeEdits: (nodeId: string) => void;
  cacheNodeDelete: (nodeId: string) => void;
  clearNodeDelete: (nodeId: string) => void;
  pushServerAction: (action: TrackedAction) => void;
  undo: () => void;
  redo: () => void;
  clearReverseAction: () => void;
  toggleAnnotations: () => void;
  cycleCanvasColorMode: () => void;
  reset: () => void;
}

function makeSnapshot(state: WorkflowEditorState): WorkflowSnapshot {
  return {
    nodeEdits: state.nodeEdits,
    deletedNodeIds: state.deletedNodeIds,
  };
}

const initialState = {
  selectedNodeId: null as string | null,
  selectedNodeIds: [] as string[],
  selectedEdgeId: null as string | null,
  mode: "view" as EditorMode,
  pendingEdgeSource: null as string | null,
  nodeEdits: {} as NodeEditCache,
  deletedNodeIds: [] as string[],
  undoStack: [] as UndoEntry[],
  redoStack: [] as UndoEntry[],
  _reverseAction: null as TrackedAction | null,
  _undoLastTime: 0,
  showAnnotations: true,
  canvasColorMode: "system" as "system" | "light" | "dark",
};

export const useWorkflowEditorStore = create<WorkflowEditorState>((set) => ({
  ...initialState,

  selectNode: (id) =>
    set({
      selectedNodeId: id,
      selectedNodeIds: id ? [id] : [],
      selectedEdgeId: null,
    }),

  setSelectedNodeIds: (ids) =>
    set({
      selectedNodeIds: ids,
      selectedNodeId: ids.length === 1 ? ids[0] : null,
    }),

  selectEdge: (id) =>
    set({
      selectedEdgeId: id,
      selectedNodeId: null,
      selectedNodeIds: [],
    }),

  setMode: (mode) =>
    set({
      mode,
      pendingEdgeSource: mode === "connect" ? undefined : null,
    }),

  setPendingEdgeSource: (id) => set({ pendingEdgeSource: id }),

  cacheNodeEdits: (nodeId, edits) =>
    set((state) => {
      const now = Date.now();
      const shouldSnapshot = now - state._undoLastTime > SNAPSHOT_INTERVAL;

      const existing = state.nodeEdits[nodeId] ?? {};
      const merged = { ...existing, ...edits };
      if (
        existing.format_schema && typeof existing.format_schema === "object" && !Array.isArray(existing.format_schema) &&
        edits.format_schema && typeof edits.format_schema === "object" && !Array.isArray(edits.format_schema)
      ) {
        merged.format_schema = {
          ...(existing.format_schema as Record<string, unknown>),
          ...(edits.format_schema as Record<string, unknown>),
        };
      }

      return {
        ...(shouldSnapshot
          ? {
              undoStack: [...state.undoStack, { snapshot: makeSnapshot(state) }].slice(-MAX_UNDO_STACK),
              redoStack: [],
            }
          : {}),
        _undoLastTime: now,
        nodeEdits: { ...state.nodeEdits, [nodeId]: merged },
      };
    }),

  clearNodeEdits: (nodeId) =>
    set((state) => {
      const next = { ...state.nodeEdits };
      delete next[nodeId];
      return { nodeEdits: next };
    }),

  cacheNodeDelete: (nodeId) =>
    set((state) => {
      if (state.deletedNodeIds.includes(nodeId)) return state;
      const now = Date.now();
      const shouldSnapshot = now - state._undoLastTime > SNAPSHOT_INTERVAL;
      return {
        ...(shouldSnapshot
          ? {
              undoStack: [...state.undoStack, { snapshot: makeSnapshot(state) }].slice(-MAX_UNDO_STACK),
              redoStack: [],
            }
          : {}),
        _undoLastTime: now,
        deletedNodeIds: [...state.deletedNodeIds, nodeId],
      };
    }),

  clearNodeDelete: (nodeId) =>
    set((state) => ({
      deletedNodeIds: state.deletedNodeIds.filter((id) => id !== nodeId),
    })),

  pushServerAction: (action) =>
    set((state) => ({
      undoStack: [...state.undoStack, { snapshot: makeSnapshot(state), action }].slice(-MAX_UNDO_STACK),
      redoStack: [],
      _undoLastTime: Date.now(),
    })),

  undo: () =>
    set((state) => {
      if (state.undoStack.length === 0) return state;
      const entry = state.undoStack[state.undoStack.length - 1];
      if (!entry) return state;
      return {
        undoStack: state.undoStack.slice(0, -1),
        redoStack: [...state.redoStack, { snapshot: makeSnapshot(state), action: entry.action }],
        nodeEdits: entry.snapshot.nodeEdits,
        deletedNodeIds: entry.snapshot.deletedNodeIds,
        _reverseAction: entry.action ?? null,
        _undoLastTime: Date.now(),
      };
    }),

  redo: () =>
    set((state) => {
      if (state.redoStack.length === 0) return state;
      const entry = state.redoStack[state.redoStack.length - 1];
      if (!entry) return state;
      return {
        redoStack: state.redoStack.slice(0, -1),
        undoStack: [...state.undoStack, { snapshot: makeSnapshot(state), action: entry.action }],
        nodeEdits: entry.snapshot.nodeEdits,
        deletedNodeIds: entry.snapshot.deletedNodeIds,
        _reverseAction: entry.action ?? null,
        _undoLastTime: Date.now(),
      };
    }),

  clearReverseAction: () => set({ _reverseAction: null }),

  toggleAnnotations: () => set((state) => ({ showAnnotations: !state.showAnnotations })),

  cycleCanvasColorMode: () =>
    set((state) => ({
      canvasColorMode:
        state.canvasColorMode === "system"
          ? "light"
          : state.canvasColorMode === "light"
            ? "dark"
            : "system",
    })),

  reset: () => set({ ...initialState, selectedNodeIds: [], selectedNodeId: null }),
}));
