"use client";

import { useQuery } from "@tanstack/react-query";
import { DAGCanvas } from "../../workflows/components";
import { workflowNodesOptions, workflowEdgesOptions, workflowNodeRunsOptions } from "@multica/core/workflows/queries";

const STATUS_COLORS: Record<string, string> = {
  pending: "#6b7280",
  format_checking: "#f59e0b",
  format_ok: "#f59e0b",
  format_failed: "#ef4444",
  worker_assigned: "#f59e0b",
  working: "#3b82f6",
  awaiting_critic: "#3b82f6",
  critic_reviewing: "#3b82f6",
  critic_approved: "#22c55e",
  critic_rework: "#f59e0b",
  completed: "#22c55e",
  failed: "#ef4444",
  blocked: "#f59e0b",
  skipped: "#6b7280",
  cancelled: "#6b7280",
};

function getNodeStatusColor(status: string): string {
  return STATUS_COLORS[status] ?? "#6b7280";
}

export function WorkflowDagViewer({
  workflowId,
  runId,
  wsId,
}: {
  workflowId: string;
  runId: string | null;
  wsId: string;
}) {
  const { data: nodes = [] } = useQuery(workflowNodesOptions(wsId, workflowId));
  const { data: edges = [] } = useQuery(workflowEdgesOptions(wsId, workflowId));
  const { data: nodeRuns = [] } = useQuery({
    ...workflowNodeRunsOptions(wsId, workflowId, runId ?? ""),
    enabled: !!runId,
  });

  const nodeStatusColors: Record<string, string> = {};
  for (const nr of nodeRuns) {
    nodeStatusColors[nr.workflow_node_id] = getNodeStatusColor(nr.status);
  }

  const completedCount = nodeRuns.filter(
    (nr) => nr.status === "completed" || nr.status === "critic_approved" || nr.status === "skipped",
  ).length;
  const totalCount = nodes.length;

  return (
    <div>
      <div className="text-sm text-muted-foreground mb-2">
        {runId
          ? `${completedCount}/${totalCount} nodes completed`
          : "Workflow assigned — run will start automatically"}
      </div>
      <div className="h-[300px] overflow-hidden rounded-lg border bg-card">
        <DAGCanvas
          nodes={nodes}
          edges={edges}
          nodeStatusColors={nodeStatusColors}
        />
      </div>
    </div>
  );
}
