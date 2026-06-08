"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Plus, Wand, Trash2, Power, ArrowLeft, Undo2, Redo2, Sun, Moon, Monitor } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  workflowDetailOptions,
  workflowNodesOptions,
  workflowEdgesOptions,
  useCreateNode,
  useUpdateNode,
  useCreateEdge,
  useUpdateWorkflow,
  useDeleteWorkflow,
  useDeleteEdge,
  useDeleteNode,
} from "@multica/core/workflows/queries";
import { useWorkflowEditorStore } from "@multica/core/workflows/store";
import { useNavigation } from "../../navigation";
import { useWorkspacePaths } from "@multica/core/paths";
import { PageHeader } from "../../layout/page-header";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { useT } from "../../i18n";
import { ReactFlowProvider } from "@xyflow/react";
import { DAGCanvas } from "./dag-canvas";
import { NodeConfigPanel } from "./node-config-panel";
import { computeAutoLayout } from "./layout";
import type { WorkflowStatus } from "@multica/core/types";

interface WorkflowDetailPageProps {
  workflowId: string;
}

export function WorkflowDetailPage({ workflowId: id }: WorkflowDetailPageProps) {
  const { t } = useT("workflows");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const navigation = useNavigation();

  const selectedNodeIds = useWorkflowEditorStore((s) => s.selectedNodeIds);
  const mode = useWorkflowEditorStore((s) => s.mode);
  const setMode = useWorkflowEditorStore((s) => s.setMode);
  const nodeEdits = useWorkflowEditorStore((s) => s.nodeEdits);
  const clearNodeEdits = useWorkflowEditorStore((s) => s.clearNodeEdits);
  const deletedNodeIds = useWorkflowEditorStore((s) => s.deletedNodeIds);
  const clearNodeDelete = useWorkflowEditorStore((s) => s.clearNodeDelete);
  const undoStack = useWorkflowEditorStore((s) => s.undoStack);
  const redoStack = useWorkflowEditorStore((s) => s.redoStack);
  const undo = useWorkflowEditorStore((s) => s.undo);
  const redo = useWorkflowEditorStore((s) => s.redo);
  const reverseAction = useWorkflowEditorStore((s) => s._reverseAction);
  const clearReverseAction = useWorkflowEditorStore((s) => s.clearReverseAction);

  const canvasColorMode = useWorkflowEditorStore((s) => s.canvasColorMode);
  const cycleCanvasColorMode = useWorkflowEditorStore((s) => s.cycleCanvasColorMode);

  useEffect(() => {
    useWorkflowEditorStore.getState().reset();
  }, [id]);

  // Handle reversal of server actions when undo/redo is triggered
  useEffect(() => {
    if (!reverseAction) return;
    const action = reverseAction;
    clearReverseAction();

    (async () => {
      try {
        if (action.type === "create-edge") {
          // Undo edge create → delete the edge
          await deleteEdgeMutation.mutateAsync(action.edgeId!);
        } else if (action.type === "delete-edge") {
          // Undo edge delete → re-create the edge
          await createEdgeMutation.mutateAsync({
            source_node_id: action.sourceNodeId!,
            target_node_id: action.targetNodeId!,
          });
        } else if (action.type === "create-node") {
          // Undo node create → delete the node
          await deleteNodeMutation.mutateAsync(action.nodeId!);
        }
      } catch {
        // silent — the snapshot restore already happened
      }
    })();
  }, [reverseAction]);
  // eslint-disable-next-line react-hooks/exhaustive-deps

  const { data: workflow, isLoading } = useQuery(workflowDetailOptions(wsId, id!));
  const { data: nodes = [] } = useQuery(workflowNodesOptions(wsId, id!));
  const { data: edges = [] } = useQuery(workflowEdgesOptions(wsId, id!));

  const createNodeMutation = useCreateNode(wsId, id!);
  const updateNodeMutation = useUpdateNode(wsId, id!);
  const createEdgeMutation = useCreateEdge(wsId, id!);
  const deleteEdgeMutation = useDeleteEdge(wsId, id!);
  const deleteNodeMutation = useDeleteNode(wsId, id!);
  const updateWorkflowMutation = useUpdateWorkflow(wsId);
  const deleteWorkflowMutation = useDeleteWorkflow(wsId);

  // Merge cached edits into nodes for instant visual feedback.
  // Memoized to keep the array reference stable across re-renders triggered
  // by WebSocket status pushes — prevents ReactFlow from resetting drag positions.
  // Also filters out nodes that have been marked for deletion.
  const displayNodes = useMemo(
    () =>
      nodes
        .filter((n) => !deletedNodeIds.includes(n.id))
        .map((n) => {
          const edits = nodeEdits[n.id];
          return edits ? { ...n, ...edits } : n;
        }),
    [nodes, nodeEdits, deletedNodeIds],
  );

  // Only show config panel when exactly 1 node is selected
  const selectedNode = selectedNodeIds.length === 1
    ? (displayNodes.find((n) => n.id === selectedNodeIds[0]) ?? null)
    : null;

  const queryClient = useQueryClient();

  const [editingTitle, setEditingTitle] = useState(false);
  const [draftTitle, setDraftTitle] = useState("");
  const [saving, setSaving] = useState(false);

  const handleNodeMoved = useCallback((nodeId: string, x: number, y: number) => {
    useWorkflowEditorStore.getState().cacheNodeEdits(nodeId, { position_x: x, position_y: y });
  }, []);

  const handleEdgeCreate = useCallback(async (sourceNodeId: string, targetNodeId: string) => {
    try {
      const result = await createEdgeMutation.mutateAsync({ source_node_id: sourceNodeId, target_node_id: targetNodeId });
      useWorkflowEditorStore.getState().pushServerAction({ type: "create-edge", edgeId: result.id });
      toast.success(t(($) => $.edge.toast_created));
    } catch {
      toast.error(t(($) => $.edge.toast_create_failed));
    }
  }, [createEdgeMutation, t]);

  const handleEdgeDelete = useCallback((edgeId: string) => {
    const edge = edges.find((e) => e.id === edgeId);
    useWorkflowEditorStore.getState().pushServerAction({
      type: "delete-edge",
      edgeId,
      sourceNodeId: edge?.source_node_id ?? "",
      targetNodeId: edge?.target_node_id ?? "",
    });
    deleteEdgeMutation.mutate(edgeId);
  }, [deleteEdgeMutation.mutate, edges]);

  const handleAddNode = async (type: string, x: number, y: number) => {
    try {
      const isAnnotation = type === "annotation";
      const formatSchema: Record<string, unknown> = isAnnotation
        ? { type: "annotation" }
        : { shape: type };
      const result = await createNodeMutation.mutateAsync({
        title: isAnnotation ? "Note" : "New Node",
        worker_type: "human",
        critic_type: "human",
        position_x: Math.round(x),
        position_y: Math.round(y),
        format_schema: formatSchema,
      });
      useWorkflowEditorStore.getState().pushServerAction({ type: "create-node", nodeId: result.id });
    } catch {
      // silent
    }
  };

  const handleSave = async () => {
    if (!workflow) return;
    try {
      await updateWorkflowMutation.mutateAsync({ id: id!, title: workflow.title, description: workflow.description });
      // Save all pending node edits
      for (const [nodeId, edits] of Object.entries(nodeEdits)) {
        updateNodeMutation.mutate({ nodeId, ...edits });
      }
      // Delete nodes marked for removal
      for (const nodeId of deletedNodeIds) {
        deleteNodeMutation.mutate(nodeId);
      }
      // Clear undo/redo — saved changes are committed, not undoable.
      // Node edits and deleted markers are intentionally NOT cleared here:
      // keeping them ensures displayNodes = nodes + edits always shows
      // the user's latest positions, preventing a flash of the pre-edit
      // layout while server mutations are in flight.
      useWorkflowEditorStore.setState({ undoStack: [], redoStack: [] });
      toast.success(t(($) => $.detail.toast_saved));
    } catch {
      toast.error(t(($) => $.detail.toast_save_failed));
    }
  };

  const handleDeleteWorkflow = async () => {
    if (!confirm("Delete this workflow? All nodes, edges and runs will be permanently deleted.")) return;
    try {
      await deleteWorkflowMutation.mutateAsync(id!);
      navigation.push(wsPaths.workflows());
    } catch {
      toast.error("Failed to delete workflow");
    }
  };

  const handleActivateWorkflow = async () => {
    if (!workflow) return;
    const newStatus = workflow.status === "active" ? "draft" : "active";
    try {
      await updateWorkflowMutation.mutateAsync({ id: id!, status: newStatus as WorkflowStatus });
      toast.success(newStatus === "active" ? "Workflow activated" : "Workflow deactivated");
    } catch (err: any) {
      toast.error(err?.message || "Failed to update workflow status");
    }
  };

  const handleAutoLayout = async () => {
    const layout = computeAutoLayout(nodes, edges);
    queryClient.setQueryData<typeof nodes>(workflowNodesOptions(wsId, id!).queryKey, (old) => {
      if (!old) return old;
      const posMap = new Map(layout.map((l) => [l.nodeId, { x: l.x, y: l.y }]));
      return old.map((n) => {
        const p = posMap.get(n.id);
        return p ? { ...n, position_x: p.x, position_y: p.y } : n;
      });
    });
    // Cache positions — saved on Done
    for (const { nodeId: nid, x, y } of layout) {
      useWorkflowEditorStore.getState().cacheNodeEdits(nid, { position_x: x, position_y: y });
    }
  };

  if (!id) return null;

  if (isLoading) {
    return (
      <div className="flex h-full items-center justify-center">
        <Skeleton className="h-[400px] w-[600px]" />
      </div>
    );
  }

  if (!workflow) {
    return (
      <div className="flex h-full items-center justify-center">
        <p className="text-sm text-muted-foreground">{t(($) => $.detail.not_found)}</p>
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col">
      {/* Toolbar */}
      <PageHeader className="justify-between px-5 shrink-0">
        <div className="flex items-center gap-2 min-w-0">
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7 shrink-0"
            onClick={async () => {
              const hasEdits = Object.keys(nodeEdits).length > 0 || deletedNodeIds.length > 0;
              if (hasEdits && mode === "edit") {
                const save = confirm("You have unsaved changes. Save before leaving?");
                if (save) {
                  await handleSave();
                } else {
                  for (const k of Object.keys(nodeEdits)) clearNodeEdits(k);
                  for (const nid of deletedNodeIds) clearNodeDelete(nid);
                }
              }
              useWorkflowEditorStore.getState().reset();
              navigation.push(wsPaths.workflows());
            }}
            title="Back to workflows"
          >
            <ArrowLeft className="h-4 w-4" />
          </Button>
          {editingTitle ? (
            <input
              className="h-7 px-2 text-sm font-medium border rounded bg-background w-48"
              value={draftTitle}
              onChange={(e) => setDraftTitle(e.currentTarget.value)}
              onBlur={async () => {
                setEditingTitle(false);
                if (draftTitle && draftTitle !== workflow?.title) {
                  await updateWorkflowMutation.mutateAsync({ id: id!, title: draftTitle });
                }
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter") (e.target as HTMLInputElement).blur();
                if (e.key === "Escape") { setDraftTitle(workflow?.title ?? ""); setEditingTitle(false); }
              }}
              autoFocus
            />
          ) : (
            <h1
              className="text-sm font-medium truncate cursor-pointer hover:text-primary transition-colors"
              onClick={() => { setDraftTitle(workflow?.title ?? ""); setEditingTitle(true); }}
              title="Click to rename"
            >
              {workflow.title}
            </h1>
          )}
          <Badge variant="secondary" className="text-[10px] px-1.5 h-4 shrink-0">
            {t(($) => ($.status as Record<string, string>)[workflow.status as WorkflowStatus] ?? workflow.status)}
          </Badge>
        </div>
        <div className="flex items-center gap-1">
          <Button
            variant={mode === "view" ? "outline" : "secondary"}
            size="sm"
            className="h-8 text-sm px-3"
            onClick={async () => {
              if (mode === "edit") {
                setSaving(true);
                try {
                  await handleSave();
                  useWorkflowEditorStore.setState({ selectedNodeId: null, selectedNodeIds: [], selectedEdgeId: null });
                } finally {
                  setSaving(false);
                }
              }
              setMode(mode === "view" ? "edit" : "view");
            }}
          >
            {mode === "view" ? t(($) => $.detail.toolbar.edit) : "Done"}
          </Button>
          <Button
            size="sm"
            variant="outline"
            onClick={handleAutoLayout}
            className="gap-1"
            title="Auto layout"
          >
            <Wand className="h-3.5 w-3.5" />
          </Button>
          <Button size="sm" variant="outline" onClick={handleDeleteWorkflow} className="text-destructive hover:text-destructive">
            <Trash2 className="h-3.5 w-3.5" />
          </Button>
          <Button
            size="sm"
            variant="outline"
            onClick={cycleCanvasColorMode}
            className="h-8 w-8 p-0"
            title={
              canvasColorMode === "system"
                ? t(($) => $.detail.canvas_theme_system)
                : canvasColorMode === "light"
                  ? t(($) => $.detail.canvas_theme_light)
                  : t(($) => $.detail.canvas_theme_dark)
            }
          >
            {canvasColorMode === "system" ? (
              <Monitor className="h-3.5 w-3.5" />
            ) : canvasColorMode === "light" ? (
              <Sun className="h-3.5 w-3.5" />
            ) : (
              <Moon className="h-3.5 w-3.5" />
            )}
          </Button>

          {mode === "edit" && (
            <>
              <Button
                size="sm"
                variant="outline"
                onClick={undo}
                disabled={undoStack.length === 0}
                className="h-8 w-8 p-0"
                title="Undo (Ctrl+Z)"
              >
                <Undo2 className="h-3.5 w-3.5" />
              </Button>
              <Button
                size="sm"
                variant="outline"
                onClick={redo}
                disabled={redoStack.length === 0}
                className="h-8 w-8 p-0"
                title="Redo (Ctrl+Shift+Z)"
              >
                <Redo2 className="h-3.5 w-3.5" />
              </Button>
            </>
          )}
          <Button
            size="sm"
            variant={workflow?.status === "active" ? "secondary" : "default"}
            onClick={handleActivateWorkflow}
            disabled={updateWorkflowMutation.isPending}
          >
            <Power className="h-3.5 w-3.5 mr-1" />
            {workflow?.status === "active" ? t(($) => $.detail.deactivate) : t(($) => $.detail.activate)}
          </Button>
        </div>
      </PageHeader>

      {/* Main content area */}
      <div className="flex flex-1 min-h-0 relative">
        {/* DAG canvas */}
        <div className="flex-1 relative bg-muted/20">
          {saving && (
            <div className="absolute inset-0 z-50 flex items-center justify-center bg-background/60 backdrop-blur-sm">
              <div className="flex flex-col items-center gap-3">
                <svg className="animate-spin h-8 w-8 text-primary" viewBox="0 0 24 24">
                  <circle cx="12" cy="12" r="10" fill="none" stroke="currentColor" strokeWidth="3" strokeDasharray="40 60" />
                </svg>
                <span className="text-sm text-muted-foreground">Saving...</span>
              </div>
            </div>
          )}
          {nodes.length === 0 ? (
            <div className="absolute inset-0 flex flex-col items-center justify-center gap-3">
              <p className="text-sm text-muted-foreground">{t(($) => $.detail.no_nodes)}</p>
              {mode === "edit" && <Button size="sm" variant="outline" onClick={() => handleAddNode("rectangle", 200, 200)}>
                <Plus className="h-3.5 w-3.5 mr-1" />
                {t(($) => $.detail.add_node)}
              </Button>}
            </div>
          ) : (
            <ReactFlowProvider>
              <DAGCanvas
                nodes={displayNodes}
                edges={edges}
                onNodeDragStop={handleNodeMoved}
                onEdgeCreate={handleEdgeCreate}
                onEdgeDelete={handleEdgeDelete}
                onNodeCreate={handleAddNode}
              />
            </ReactFlowProvider>
          )}
          {/* Add node button (floating, top-left) */}
          {nodes.length > 0 && mode === "edit" && (
            <Button
              size="icon"
              variant="outline"
              className="absolute top-3 left-3 h-9 w-9"
              onClick={() => handleAddNode("rectangle", 200, 200)}
              title={t(($) => $.detail.add_node)}
            >
              <Plus className="h-4 w-4" />
            </Button>
          )}
        </div>

        {/* Right sidebar: config panel */}
        {selectedNode && (
          <div className="w-96 shrink-0">
            <NodeConfigPanel
              node={selectedNode}
              workflowId={id!}
              nodes={displayNodes}
              onClose={() => useWorkflowEditorStore.getState().selectNode(null)}
            />
          </div>
        )}
      </div>
    </div>
  );
}
