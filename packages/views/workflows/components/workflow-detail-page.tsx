"use client";

import { useCallback, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Plus, Wand, Trash2, Power, ArrowLeft } from "lucide-react";
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
} from "@multica/core/workflows/queries";
import { useWorkflowEditorStore } from "@multica/core/workflows/store";
import { useNavigation } from "../../navigation";
import { useWorkspacePaths } from "@multica/core/paths";
import { PageHeader } from "../../layout/page-header";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { useT } from "../../i18n";
import { DAGCanvas } from "./dag-canvas";
import { NodeConfigPanel } from "./node-config-panel";
import { computeAutoLayout } from "./dag-canvas";
import type { WorkflowStatus } from "@multica/core/types";

interface WorkflowDetailPageProps {
  workflowId: string;
}

export function WorkflowDetailPage({ workflowId: id }: WorkflowDetailPageProps) {
  const { t } = useT("workflows");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const navigation = useNavigation();

  const selectedNodeId = useWorkflowEditorStore((s) => s.selectedNodeId);
  const mode = useWorkflowEditorStore((s) => s.mode);
  const setMode = useWorkflowEditorStore((s) => s.setMode);
  const nodeEdits = useWorkflowEditorStore((s) => s.nodeEdits);
  const clearNodeEdits = useWorkflowEditorStore((s) => s.clearNodeEdits);

  const { data: workflow, isLoading } = useQuery(workflowDetailOptions(wsId, id!));
  const { data: nodes = [] } = useQuery(workflowNodesOptions(wsId, id!));
  const { data: edges = [] } = useQuery(workflowEdgesOptions(wsId, id!));

  const createNodeMutation = useCreateNode(wsId, id!);
  const updateNodeMutation = useUpdateNode(wsId, id!);
  const createEdgeMutation = useCreateEdge(wsId, id!);
  const deleteEdgeMutation = useDeleteEdge(wsId, id!);
  const updateWorkflowMutation = useUpdateWorkflow(wsId);
  const deleteWorkflowMutation = useDeleteWorkflow(wsId);

  const selectedNode = nodes.find((n) => n.id === selectedNodeId) ?? null;

  const queryClient = useQueryClient();

  const [editingTitle, setEditingTitle] = useState(false);
  const [draftTitle, setDraftTitle] = useState("");

  const handleNodeMoved = useCallback((nodeId: string, x: number, y: number) => {
    queryClient.setQueryData<typeof nodes>(workflowNodesOptions(wsId, id!).queryKey, (old) => {
      if (!old) return old;
      return old.map((n) => n.id === nodeId ? { ...n, position_x: x, position_y: y } : n);
    });
    useWorkflowEditorStore.getState().cacheNodeEdits(nodeId, { position_x: x, position_y: y });
  }, [wsId, id, queryClient]);

  const handleEdgeCreate = useCallback(async (sourceNodeId: string, targetNodeId: string) => {
    try {
      await createEdgeMutation.mutateAsync({ source_node_id: sourceNodeId, target_node_id: targetNodeId });
      toast.success(t(($) => $.edge.toast_created));
    } catch {
      toast.error(t(($) => $.edge.toast_create_failed));
    }
  }, [createEdgeMutation, t]);

  const handleEdgeDelete = useCallback((edgeId: string) => {
    deleteEdgeMutation.mutate(edgeId);
  }, [deleteEdgeMutation]);

  const handleAddNode = async () => {
    try {
      await createNodeMutation.mutateAsync({
        title: "New Node",
        worker_type: "human",
        critic_type: "human",
        position_x: 200 + Math.random() * 200,
        position_y: 200 + Math.random() * 200,
      });
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
        clearNodeEdits(nodeId);
      }
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
    for (const { nodeId: nid, x, y } of layout) {
      updateNodeMutation.mutate({ nodeId: nid, position_x: x, position_y: y });
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
              const hasEdits = Object.keys(nodeEdits).length > 0;
              if (hasEdits && mode === "edit") {
                const save = confirm("You have unsaved changes. Save before leaving?");
                if (save) {
                  await handleSave();
                } else {
                  for (const k of Object.keys(nodeEdits)) clearNodeEdits(k);
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
              if (mode === "edit") await handleSave();
              setMode(mode === "view" ? "edit" : "view");
            }}
          >
            {mode === "view" ? t(($) => $.detail.toolbar.edit) : "Done"}
          </Button>
          {mode === "edit" && (
            <>
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
      <div className="flex flex-1 min-h-0">
        {/* DAG canvas */}
        <div className="flex-1 relative bg-muted/20">
          {nodes.length === 0 ? (
            <div className="absolute inset-0 flex flex-col items-center justify-center gap-3">
              <p className="text-sm text-muted-foreground">{t(($) => $.detail.no_nodes)}</p>
              {mode === "edit" && <Button size="sm" variant="outline" onClick={handleAddNode}>
                <Plus className="h-3.5 w-3.5 mr-1" />
                {t(($) => $.detail.add_node)}
              </Button>}
            </div>
          ) : (
            <DAGCanvas
              nodes={nodes}
              edges={edges}
              onNodeMoved={handleNodeMoved}
              onEdgeCreate={handleEdgeCreate}
              onEdgeDelete={handleEdgeDelete}
              onNodeDoubleClick={() => {
                setMode("edit");
              }}
            />
          )}
          {/* Add node button (floating) */}
          {nodes.length > 0 && mode === "edit" && (
            <Button
              size="icon"
              variant="outline"
              className="absolute top-3 left-3 h-7 w-7 rounded-full shadow"
              onClick={handleAddNode}
            >
              <Plus className="h-3.5 w-3.5" />
            </Button>
          )}
        </div>

        {/* Right sidebar: config panel (absolute overlay — don't resize DAG) */}
        {selectedNode && (
          <div className="absolute right-0 top-0 bottom-0 w-96 z-10 h-full">
            <NodeConfigPanel
              node={selectedNode}
              workflowId={id!}
              onClose={() => useWorkflowEditorStore.getState().selectNode(null)}
            />
          </div>
        )}
      </div>
    </div>
  );
}
