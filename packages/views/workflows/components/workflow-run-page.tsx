"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  workflowRunOptions,
  workflowNodesOptions,
  workflowEdgesOptions,
  workflowNodeRunsOptions,
  useCancelWorkflowRun,
} from "@multica/core/workflows/queries";
import { PageHeader } from "../../layout/page-header";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { useT } from "../../i18n";
import { DAGCanvas } from "./dag-canvas";
import { NodeRunCard } from "./node-run-card";
import type { WorkflowRunStatus, NodeRunStatus } from "@multica/core/types";

const STATUS_COLOR: Record<NodeRunStatus, string> = {
  pending: "fill-muted stroke-muted",
  format_checking: "fill-blue-500/30 stroke-blue-500",
  format_ok: "fill-emerald-500/20 stroke-emerald-500",
  format_failed: "fill-red-500/30 stroke-red-500",
  worker_assigned: "fill-amber-500/20 stroke-amber-500",
  working: "fill-blue-500/30 stroke-blue-500",
  awaiting_critic: "fill-amber-500/20 stroke-amber-500",
  critic_reviewing: "fill-purple-500/20 stroke-purple-500",
  critic_approved: "fill-emerald-500/20 stroke-emerald-500",
  critic_rework: "fill-orange-500/20 stroke-orange-500",
  completed: "fill-emerald-500/30 stroke-emerald-500",
  failed: "fill-red-500/30 stroke-red-500",
  blocked: "fill-red-500/30 stroke-red-500",
  skipped: "fill-muted stroke-muted",
  cancelled: "fill-muted stroke-muted",
};

interface WorkflowRunPageProps {
  workflowId: string;
  runId: string;
}

export function WorkflowRunPage({ workflowId, runId }: WorkflowRunPageProps) {
  const { t } = useT("workflows");
  const wsId = useWorkspaceId();

  const { data: run, isLoading: runLoading } = useQuery(workflowRunOptions(wsId, workflowId, runId));
  const { data: nodes = [], isLoading: nodesLoading } = useQuery(workflowNodesOptions(wsId, workflowId));
  const { data: edges = [] } = useQuery(workflowEdgesOptions(wsId, workflowId));
  const { data: nodeRuns = [], isLoading: nodeRunsLoading } = useQuery(workflowNodeRunsOptions(wsId, workflowId, runId));

  const cancelMutation = useCancelWorkflowRun(wsId);

  const isLoading = runLoading || nodesLoading || nodeRunsLoading;

  const nodeRunByNodeId = new Map(nodeRuns.map((nr) => [nr.workflow_node_id, nr]));

  const nodeStatusColors: Record<string, string> = {};
  for (const node of nodes) {
    const nr = nodeRunByNodeId.get(node.id);
    if (nr) {
      nodeStatusColors[node.id] = STATUS_COLOR[nr.status as NodeRunStatus] ?? "fill-muted stroke-muted";
    }
  }

  const handleCancel = () => {
    cancelMutation.mutate({ workflowId, runId });
  };

  if (isLoading) {
    return (
      <div className="flex h-full items-center justify-center">
        <Skeleton className="h-[400px] w-[600px]" />
      </div>
    );
  }

  if (!run) {
    return (
      <div className="flex h-full items-center justify-center">
        <p className="text-sm text-muted-foreground">{t(($) => $.detail.not_found)}</p>
      </div>
    );
  }

  const canCancel = run.status === "running";

  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      <PageHeader className="justify-between px-5 shrink-0">
        <div className="flex items-center gap-2 min-w-0">
          <h1 className="text-sm font-medium truncate">{run.workflow_title}</h1>
          <Badge variant="secondary" className="text-[10px] px-1.5 h-4">
            {t(($) => ($.run.status as Record<string, string>)[run.status as WorkflowRunStatus] ?? run.status)}
          </Badge>
        </div>
        <div className="flex items-center gap-2">
          {canCancel && (
            <Button
              size="sm"
              variant="outline"
              onClick={handleCancel}
              disabled={cancelMutation.isPending}
            >
              {cancelMutation.isPending ? t(($) => $.run.cancelling) : t(($) => $.run.cancel)}
            </Button>
          )}
        </div>
      </PageHeader>

      {/* Content: DAG + Node Run list */}
      <div className="flex flex-1 min-h-0">
        <div className="flex-1 bg-muted/20">
          {nodes.length > 0 ? (
            <DAGCanvas
              nodes={nodes}
              edges={edges}
              nodeStatusColors={nodeStatusColors}
            />
          ) : (
            <div className="flex items-center justify-center h-full">
              <p className="text-sm text-muted-foreground">{t(($) => $.detail.no_nodes)}</p>
            </div>
          )}
        </div>
        <div className="w-80 shrink-0 border-l bg-card overflow-y-auto p-3 space-y-2">
          <h3 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider px-1">
            Node Runs
          </h3>
          {nodeRuns.map((nr) => (
            <NodeRunCard
              key={nr.id}
              nodeRun={nr}
              maxRetries={3}
            />
          ))}
        </div>
      </div>
    </div>
  );
}
