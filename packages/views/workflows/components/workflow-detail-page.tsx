"use client";

import { useCallback } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Play, Save, Plus, Wand, Trash2 } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  workflowDetailOptions,
  workflowNodesOptions,
  workflowEdgesOptions,
  workflowRunsOptions,
  useCreateNode,
  useUpdateNode,
  useCreateEdge,
  useUpdateWorkflow,
  useStartWorkflowRun,
  useDeleteWorkflow,
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

  const { data: workflow, isLoading } = useQuery(workflowDetailOptions(wsId, id!));
  const { data: nodes = [] } = useQuery(workflowNodesOptions(wsId, id!));
  const { data: edges = [] } = useQuery(workflowEdgesOptions(wsId, id!));
  const { data: runs = [] } = useQuery(workflowRunsOptions(wsId, id!));

  const createNodeMutation = useCreateNode(wsId, id!);
  const updateNodeMutation = useUpdateNode(wsId, id!);
  const createEdgeMutation = useCreateEdge(wsId, id!);
  const updateWorkflowMutation = useUpdateWorkflow(wsId);
  const startRunMutation = useStartWorkflowRun(wsId);
  const deleteWorkflowMutation = useDeleteWorkflow(wsId);

  const selectedNode = nodes.find((n) => n.id === selectedNodeId) ?? null;

  const handleNodeMoved = useCallback((nodeId: string, x: number, y: number) => {
    updateNodeMutation.mutate({ nodeId, position_x: x, position_y: y });
  }, [updateNodeMutation]);

  const handleEdgeCreate = useCallback(async (sourceNodeId: string, targetNodeId: string) => {
    try {
      await createEdgeMutation.mutateAsync({ source_node_id: sourceNodeId, target_node_id: targetNodeId });
      toast.success(t(($) => $.edge.toast_created));
    } catch {
      toast.error(t(($) => $.edge.toast_create_failed));
    }
  }, [createEdgeMutation, t]);

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

  const handleStartRun = async () => {
    try {
      const run = await startRunMutation.mutateAsync({ workflowId: id! });
      toast.success(t(($) => $.detail.toast_run_started));
      navigation.push(wsPaths.workflowRunDetail(id!, run.id));
    } catch {
      toast.error(t(($) => $.detail.toast_run_failed));
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
          <h1 className="text-sm font-medium truncate">{workflow.title}</h1>
          <Badge variant="secondary" className="text-[10px] px-1.5 h-4 shrink-0">
            {t(($) => ($.status as Record<string, string>)[workflow.status as WorkflowStatus] ?? workflow.status)}
          </Badge>
        </div>
        <div className="flex items-center gap-1">
          <div className="flex items-center gap-0.5 border rounded-md mr-1">
            <Button
              variant={mode === "view" ? "secondary" : "ghost"}
              size="sm"
              className="h-8 text-sm px-3 rounded-r-none"
              onClick={() => setMode("view")}
            >
              {t(($) => $.detail.toolbar.view)}
            </Button>
            <Button
              variant={mode === "edit" ? "secondary" : "ghost"}
              size="sm"
              className="h-8 text-sm px-3 rounded-none"
              onClick={() => setMode("edit")}
            >
              {t(($) => $.detail.toolbar.edit)}
            </Button>
            <Button
              variant={mode === "connect" ? "secondary" : "ghost"}
              size="sm"
              className="h-8 text-sm px-3 rounded-l-none"
              onClick={() => setMode("connect")}
            >
              {t(($) => $.detail.toolbar.connect)}
            </Button>
          </div>
          <Button
            size="sm"
            variant="outline"
            onClick={handleAutoLayout}
            className="gap-1"
            title="Auto layout"
          >
            <Wand className="h-3.5 w-3.5" />
          </Button>
          <Button size="sm" variant="outline" onClick={handleSave}>
            <Save className="h-3.5 w-3.5" />
          </Button>
          <Button size="sm" variant="outline" onClick={handleDeleteWorkflow} className="text-destructive hover:text-destructive">
            <Trash2 className="h-3.5 w-3.5" />
          </Button>
          <Button
            size="sm"
            variant="outline"
            onClick={handleStartRun}
            disabled={startRunMutation.isPending}
          >
            <Play className="h-3.5 w-3.5 mr-1" />
            {startRunMutation.isPending
              ? t(($) => $.detail.starting_run)
              : t(($) => $.detail.start_run)}
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
              <Button size="sm" variant="outline" onClick={handleAddNode}>
                <Plus className="h-3.5 w-3.5 mr-1" />
                {t(($) => $.detail.add_node)}
              </Button>
            </div>
          ) : (
            <DAGCanvas
              nodes={nodes}
              edges={edges}
              onNodeMoved={handleNodeMoved}
              onEdgeCreate={handleEdgeCreate}
              onNodeDoubleClick={() => {
                setMode("edit");
              }}
            />
          )}
          {/* Add node button (floating) */}
          {nodes.length > 0 && (
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

        {/* Right sidebar: config panel or run history */}
        {selectedNode ? (
          <div className="w-96 shrink-0 h-full">
            <NodeConfigPanel
              node={selectedNode}
              workflowId={id!}
              onClose={() => useWorkflowEditorStore.getState().selectNode(null)}
            />
          </div>
        ) : (
          <div className="w-96 shrink-0 border-l bg-card hidden lg:flex flex-col">
            <div className="px-4 py-3 border-b">
              <h3 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider">
                {t(($) => $.detail.section_run_history)}
              </h3>
            </div>
            <div className="flex-1 overflow-y-auto p-3">
              {runs.length === 0 ? (
                <p className="text-sm text-muted-foreground text-center py-8">
                  {t(($) => $.detail.no_runs)}
                </p>
              ) : (
                <div className="space-y-2">
                  {runs.slice(0, 20).map((run) => (
                    <button
                      key={run.id}
                      type="button"
                      className="w-full text-left rounded-md border px-3 py-2 text-sm hover:bg-accent/40 transition-colors"
                      onClick={() => navigation.push(wsPaths.workflowRunDetail(id!, run.id))}
                    >
                      <div className="flex items-center justify-between">
                        <span className="font-medium truncate">
                          {new Date(run.started_at).toLocaleDateString()}
                        </span>
                        <Badge variant="secondary" className="text-[10px] px-1.5 h-4">
                          {t(($) => ($.run.status as Record<string, string>)[run.status] ?? run.status)}
                        </Badge>
                      </div>
                      <div className="text-muted-foreground mt-0.5">
                        {new Date(run.started_at).toLocaleTimeString()}
                      </div>
                    </button>
                  ))}
                </div>
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
