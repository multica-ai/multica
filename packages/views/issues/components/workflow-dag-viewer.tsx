"use client";

import { useQuery } from "@tanstack/react-query";
import { DAGCanvas } from "../../workflows/components";
import { workflowDetailOptions, workflowNodesOptions, workflowEdgesOptions, workflowNodeRunsOptions } from "@multica/core/workflows/queries";
import { Badge } from "@multica/ui/components/ui/badge";
import { cn } from "@multica/ui/lib/utils";

const STATUS_CONFIG: Record<string, { color: string; label: string }> = {
  pending:           { color: "#6b7280", label: "Pending" },
  format_checking:   { color: "#f59e0b", label: "Checking" },
  format_ok:         { color: "#f59e0b", label: "Format OK" },
  format_failed:     { color: "#ef4444", label: "Format Failed" },
  worker_assigned:   { color: "#f59e0b", label: "Assigned" },
  working:           { color: "#3b82f6", label: "Working" },
  awaiting_critic:   { color: "#3b82f6", label: "Awaiting Review" },
  critic_reviewing:  { color: "#3b82f6", label: "Reviewing" },
  critic_approved:   { color: "#22c55e", label: "Approved" },
  critic_rework:     { color: "#f59e0b", label: "Rework" },
  completed:         { color: "#22c55e", label: "Done" },
  failed:            { color: "#ef4444", label: "Failed" },
  blocked:           { color: "#f59e0b", label: "Blocked" },
  skipped:           { color: "#6b7280", label: "Skipped" },
  cancelled:         { color: "#6b7280", label: "Cancelled" },
};

function getStatusColor(status: string): string {
  return STATUS_CONFIG[status]?.color ?? "#6b7280";
}

function getStatusLabel(status: string): string {
  return STATUS_CONFIG[status]?.label ?? status;
}

function getRunSummary(nodeRuns: { status: string }[]): { label: string; variant: "default" | "secondary" | "destructive" | "outline" } {
  const hasBlocked = nodeRuns.some((n) => n.status === "blocked" || n.status === "format_failed");
  const hasFailed = nodeRuns.some((n) => n.status === "failed");
  const allDone = nodeRuns.length > 0 && nodeRuns.every(
    (n) => n.status === "completed" || n.status === "critic_approved" || n.status === "skipped"
  );
  const anyRunning = nodeRuns.some(
    (n) => !["completed", "critic_approved", "skipped", "failed", "cancelled", "pending", "blocked", "format_failed"].includes(n.status)
  );

  if (hasBlocked) return { label: "Blocked", variant: "destructive" };
  if (hasFailed) return { label: "Failed", variant: "destructive" };
  if (allDone) return { label: "Completed", variant: "default" };
  if (anyRunning) return { label: "In Progress", variant: "secondary" };
  return { label: "Pending", variant: "outline" };
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
  const { data: workflow } = useQuery(workflowDetailOptions(wsId, workflowId));
  const { data: nodeRuns = [] } = useQuery({
    ...workflowNodeRunsOptions(wsId, workflowId, runId ?? ""),
    enabled: !!runId,
  });

  const nodeStatusColors: Record<string, string> = {};
  const nodeStatusLabels: Record<string, string> = {};
  for (const nr of nodeRuns) {
    nodeStatusColors[nr.workflow_node_id] = getStatusColor(nr.status);
    nodeStatusLabels[nr.workflow_node_id] = getStatusLabel(nr.status);
  }

  const totalCount = nodes.length;
  const summary = getRunSummary(nodeRuns);

  return (
    <div>
      <div className="flex items-center gap-2 mb-2">
        <span className="text-sm font-medium">Workflow</span>
        {!runId ? (
          <Badge variant="outline">Not started</Badge>
        ) : (
          <Badge variant={summary.variant}>{summary.label}</Badge>
        )}
        {runId && (
          <span className="text-xs text-muted-foreground tabular-nums">
            {nodeRuns.filter((n) => n.status === "completed" || n.status === "critic_approved" || n.status === "skipped").length}/{totalCount} done
          </span>
        )}
      </div>

      {!runId && workflow && workflow.status !== "active" && (
        <div className="mb-2 px-3 py-1.5 rounded-md bg-amber-50 border border-amber-200 text-xs text-amber-700 dark:bg-amber-950 dark:border-amber-800 dark:text-amber-300">
          Workflow is <strong>{workflow.status}</strong> — activate it in the workflow detail page first.
        </div>
      )}
      {!runId && workflow && workflow.status === "active" && (
        <div className="mb-2 px-3 py-1.5 rounded-md bg-blue-50 border border-blue-200 text-xs text-blue-700 dark:bg-blue-950 dark:border-blue-800 dark:text-blue-300">
          Workflow is active — run should start when assigned. Try reassigning the issue.
        </div>
      )}

      <div className={cn("h-[400px] overflow-hidden rounded-lg border bg-card", !runId && "opacity-60")}>
        <DAGCanvas
          nodes={nodes}
          edges={edges}
          nodeStatusColors={nodeStatusColors}
          initialScale={2}
        />
      </div>

      {/* Per-node status list */}
      {runId && nodeRuns.length > 0 && (
        <div className="mt-3 space-y-1">
          {nodeRuns.map((nr) => {
            const node = nodes.find((n) => n.id === nr.workflow_node_id);
            return (
              <div key={nr.id} className="flex items-center gap-2 text-xs">
                <span
                  className="w-2 h-2 rounded-full shrink-0"
                  style={{ backgroundColor: getStatusColor(nr.status) }}
                />
                <span className="w-28 truncate text-muted-foreground">
                  {node?.title ?? nr.node_title}
                </span>
                <Badge variant="secondary" className="text-[10px] px-1.5 h-4">
                  {getStatusLabel(nr.status)}
                </Badge>
                {nr.retry_count > 0 && (
                  <span className="text-[10px] text-muted-foreground">
                    retry {nr.retry_count}
                  </span>
                )}
              </div>
            );
          })}
        </div>
      )}

      {!runId && (
        <p className="mt-2 text-xs text-muted-foreground">
          Activate the workflow in its detail page, then assign this issue. A run will start automatically.
        </p>
      )}
    </div>
  );
}
