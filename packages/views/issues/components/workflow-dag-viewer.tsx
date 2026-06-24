"use client";

import { useState, useEffect, useRef } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { DAGCanvas } from "../../workflows/components";
import { ReactFlowProvider } from "@xyflow/react";
import {
  workflowKeys,
  workflowDetailOptions,
  workflowNodesOptions,
  workflowEdgesOptions,
  workflowNodeRunsOptions,
  useCancelWorkflowRun,
} from "@multica/core/workflows/queries";
import { issueKeys } from "@multica/core/issues/queries";
import { api } from "@multica/core/api";
import { useActorName } from "@multica/core/workspace/hooks";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogHeader } from "@multica/ui/components/ui/dialog";
import { NodeRunControlActions } from "../../workflows/components/node-run-control-actions";
import { ExecutionLogSection } from "./execution-log-section";
import { cn } from "@multica/ui/lib/utils";

const STATUS_CONFIG: Record<string, { color: string; label: string }> = {
  pending:           { color: "rgba(107,114,128,0.2)", label: "Pending" },
  format_checking:   { color: "rgba(245,158,11,0.25)", label: "Checking" },
  format_ok:         { color: "rgba(245,158,11,0.25)", label: "Format OK" },
  format_failed:     { color: "rgba(239,68,68,0.25)", label: "Format Failed" },
  worker_assigned:   { color: "rgba(245,158,11,0.25)", label: "Assigned" },
  working:           { color: "rgba(59,130,246,0.25)", label: "Working" },
  awaiting_critic:   { color: "rgba(59,130,246,0.25)", label: "Awaiting Review" },
  critic_reviewing:  { color: "rgba(59,130,246,0.25)", label: "Reviewing" },
  critic_approved:   { color: "rgba(34,197,94,0.25)", label: "Approved" },
  critic_rework:     { color: "rgba(245,158,11,0.25)", label: "Rework" },
  completed:         { color: "rgba(34,197,94,0.25)", label: "Done" },
  failed:            { color: "rgba(239,68,68,0.25)", label: "Failed" },
  blocked:           { color: "rgba(245,158,11,0.25)", label: "Blocked" },
  skipped:           { color: "rgba(107,114,128,0.2)", label: "Skipped" },
  cancelled:         { color: "rgba(107,114,128,0.2)", label: "Cancelled" },
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
  const allCancelled = nodeRuns.length > 0 && nodeRuns.every((n) => n.status === "cancelled");
  const anyCancelled = nodeRuns.some((n) => n.status === "cancelled");
  const allDone = nodeRuns.length > 0 && nodeRuns.every(
    (n) => n.status === "completed" || n.status === "critic_approved" || n.status === "skipped"
  );
  const anyRunning = nodeRuns.some(
    (n) => !["completed", "critic_approved", "skipped", "failed", "cancelled", "pending", "blocked", "format_failed"].includes(n.status)
  );

  if (allCancelled) return { label: "Cancelled", variant: "outline" as const };
  if (hasBlocked) return { label: "Blocked", variant: "destructive" as const };
  if (hasFailed) return { label: "Failed", variant: "destructive" as const };
  if (allDone) return { label: "Completed", variant: "default" as const };
  if (anyRunning) return { label: "In Progress", variant: "secondary" as const };
  // Mixed terminal states (e.g. some completed + some cancelled after cancel) — treat as cancelled.
  if (anyCancelled) return { label: "Cancelled", variant: "outline" as const };
  return { label: "Pending", variant: "outline" as const };
}

function workerTypeToActorType(t: string): string {
  if (t === "human") return "member";
  if (t === "agent") return "agent";
  if (t === "squad") return "squad";
  return "member";
}

function isWorkerDone(status: string): boolean {
  return ["awaiting_critic", "critic_reviewing", "critic_approved", "completed"].includes(status);
}

function isCriticDone(status: string): boolean {
  return ["critic_approved", "completed"].includes(status);
}

function isWorkerClickable(_workerType: string, status: string): boolean {
  if (["pending", "format_checking", "format_ok"].includes(status)) return false;
  return true;
}

function isCriticClickable(_criticType: string, status: string): boolean {
  if (!["awaiting_critic", "critic_reviewing", "critic_approved", "completed"].includes(status)) return false;
  return true;
}

function isWorkerPhase(status: string): boolean {
  return ["worker_assigned", "working"].includes(status);
}

function isCriticPhase(status: string): boolean {
  return ["awaiting_critic", "critic_reviewing", "critic_approved", "critic_rework"].includes(status);
}

export function WorkflowDagViewer({
  workflowId,
  runId,
  wsId,
  parentIssueId,
  onRunningChange,
}: {
  workflowId: string;
  runId: string | null;
  wsId: string;
  parentIssueId?: string;
  /** Called when the run transitions between running and non-running. */
  onRunningChange?: (running: boolean) => void;
}) {
  const { data: nodes = [] } = useQuery(workflowNodesOptions(wsId, workflowId));
  const { data: edges = [] } = useQuery(workflowEdgesOptions(wsId, workflowId));
  const { data: workflow } = useQuery(workflowDetailOptions(wsId, workflowId));
  const { data: nodeRuns = [] } = useQuery({
    ...workflowNodeRunsOptions(wsId, workflowId, runId ?? ""),
    enabled: !!runId,
  });
  const cancelRunMutation = useCancelWorkflowRun(wsId);
  const queryClient = useQueryClient();
  const { getActorName } = useActorName();

  // Fetch child issues to map node runs → sub-issues
  const { data: childIssues } = useQuery({
    queryKey: [...issueKeys.detail(wsId, parentIssueId ?? ""), "children"],
    queryFn: () => api.listChildIssues(parentIssueId ?? ""),
    enabled: !!parentIssueId,
    select: (data) => data.issues,
  });

  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [taskLogOpen, setTaskLogOpen] = useState(false);
  const [taskLogAgentId, setTaskLogAgentId] = useState<string | null>(null);

  const subIssueByNodeRunId = new Map<string, string>();
  if (childIssues) {
    for (const child of childIssues) {
      if (child.origin_type === "workflow" && child.origin_id) {
        subIssueByNodeRunId.set(child.origin_id, child.id);
      }
    }
  }

  const nodeStatusColors: Record<string, string> = {};
  const nodeStatuses: Record<string, { status: string; isRunning: boolean; isAwaitingInput: boolean }> = {};
  const runningSet = new Set(["format_checking", "working", "critic_reviewing"]);
  for (const nr of nodeRuns) {
    nodeStatusColors[nr.workflow_node_id] = getStatusColor(nr.status);
    nodeStatuses[nr.workflow_node_id] = {
      status: getStatusLabel(nr.status),
      isRunning: runningSet.has(nr.status),
      isAwaitingInput: nr.status === "awaiting_input",
    };
  }

  const totalCount = nodes.length;
  const summary = getRunSummary(nodeRuns);

  // Auto-select the most relevant node on first load so the active task is
  // surfaced without the user needing to click the DAG.
  // Priority: running > blocked > first pending.
  const hasAutoSelectedRef = useRef(false);
  useEffect(() => {
    if (hasAutoSelectedRef.current) return;
    if (selectedNodeId) {
      hasAutoSelectedRef.current = true;
      return;
    }
    const blockedSet = new Set(["blocked"]);
    const pendingSet = new Set(["pending"]);
    const pick =
      nodeRuns.find((nr) => runningSet.has(nr.status)) ??
      nodeRuns.find((nr) => blockedSet.has(nr.status)) ??
      nodeRuns.find((nr) => pendingSet.has(nr.status));
    if (!pick) return;
    setSelectedNodeId(pick.workflow_node_id);
    hasAutoSelectedRef.current = true;
  }, [nodeRuns, selectedNodeId]);

  const handleCancel = async () => {
    if (!runId || !confirm("Cancel this workflow run? All active sub-issues will stop.")) return;
    try {
      await cancelRunMutation.mutateAsync({ workflowId, runId });
      // Immediately mark all node runs as cancelled in cache so the DAG
      // and the onRunningChange callback update without waiting for refetch.
      queryClient.setQueryData(
        workflowKeys.nodeRuns(wsId, workflowId, runId),
        (old: any) => Array.isArray(old) ? old.map((nr: any) => ({ ...nr, status: "cancelled" })) : old,
      );
      queryClient.invalidateQueries({ queryKey: workflowKeys.nodeRuns(wsId, workflowId, runId) });
      if (parentIssueId) {
        queryClient.invalidateQueries({ queryKey: issueKeys.detail(wsId, parentIssueId) });
        await queryClient.refetchQueries({ queryKey: issueKeys.detail(wsId, parentIssueId), exact: true });
      }
    } catch {
      // silent
    }
  };

  const isRunning = summary.label === "In Progress";

  useEffect(() => {
    onRunningChange?.(isRunning);
  }, [isRunning, onRunningChange]);

  const selectedNodeRun = selectedNodeId
    ? nodeRuns.find((nr) => nr.workflow_node_id === selectedNodeId) ?? null
    : null;
  const selectedNode = selectedNodeId
    ? nodes.find((n) => n.id === selectedNodeId) ?? null
    : null;

  const subIssueId = selectedNodeRun ? (subIssueByNodeRunId.get(selectedNodeRun.id) ?? null) : null;

  const taskLogAgentName = taskLogAgentId && selectedNodeRun
    ? (() => {
        const isCritic = taskLogAgentId === selectedNodeRun.critic_id;
        const type = workerTypeToActorType(isCritic ? selectedNodeRun.critic_type : selectedNodeRun.worker_type);
        return getActorName(type, taskLogAgentId);
      })()
    : null;

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
        <div className="flex-1" />
        {isRunning && runId && (
          <Button
            size="sm"
            variant="destructive"
            className="h-7 text-xs"
            onClick={handleCancel}
            disabled={cancelRunMutation.isPending}
          >
            Cancel Run
          </Button>
        )}
      </div>

      {!runId && workflow && workflow.status !== "active" && (
        <div className="mb-2 px-3 py-1.5 rounded-md bg-amber-50 border border-amber-200 text-xs text-amber-700 dark:bg-amber-950 dark:border-amber-800 dark:text-amber-300">
          Workflow is <strong>{workflow.status}</strong> — activate it in the workflow detail page first.
        </div>
      )}
      {!runId && workflow && workflow.status === "active" && (
        <div className="mb-2 px-3 py-1.5 rounded-md bg-blue-50 border border-blue-200 text-xs text-blue-700 dark:bg-blue-950 dark:border-blue-800 dark:text-blue-300">
          Workflow is active — run will start when assigned.
        </div>
      )}

      <div className={cn("h-[270px] overflow-hidden rounded-lg border bg-card", !runId && "opacity-60")}>
        <ReactFlowProvider>
          <DAGCanvas
            nodes={nodes}
            edges={edges}
            nodeStatusColors={nodeStatusColors}
            nodeStatuses={nodeStatuses}
            onNodeClick={(id) => setSelectedNodeId(id === selectedNodeId ? null : id)}
            showMiniMap={false}
          />
        </ReactFlowProvider>
      </div>

      {/* Agent execution log dialog */}
      <Dialog open={taskLogOpen} onOpenChange={setTaskLogOpen}>
        <DialogContent className="sm:max-w-lg max-h-[80vh] flex flex-col">
          <DialogHeader>
            <span className="text-sm font-medium">
              {taskLogAgentName ?? selectedNode?.title ?? "Node"} — Agent Activity
            </span>
          </DialogHeader>
          <div className="flex-1 overflow-y-auto min-h-0">
            {subIssueId ? (
              selectedNodeRun && (
                (taskLogAgentId === selectedNodeRun.worker_id && selectedNodeRun.worker_type === "human") ||
                (taskLogAgentId === selectedNodeRun.critic_id && selectedNodeRun.critic_type === "human")
              ) ? (
                <p className="text-xs text-muted-foreground py-4">
                  This is a human task. No agent execution log available. Check the sub-issue for updates.
                </p>
              ) : (
                <ExecutionLogSection issueId={subIssueId} agentId={taskLogAgentId ?? undefined} />
              )
            ) : (
              <p className="text-xs text-muted-foreground">No sub-issue found for this node.</p>
            )}
          </div>
        </DialogContent>
      </Dialog>

      {/* Selected node detail panel */}
      {selectedNodeRun && selectedNode && (
        <div className="mt-2 rounded border bg-card p-3 space-y-3">
          <div className="flex items-center gap-2">
            <span
              className="w-2 h-2 rounded-full shrink-0"
              style={{ backgroundColor: getStatusColor(selectedNodeRun.status) }}
            />
            <span className="text-sm font-medium">{selectedNode.title}</span>
            <Badge variant="secondary" className="text-[10px] px-1.5 h-4">
              {getStatusLabel(selectedNodeRun.status)}
            </Badge>
            {selectedNodeRun.retry_count > 0 && (
              <span className="text-[10px] text-muted-foreground">retry {selectedNodeRun.retry_count}</span>
            )}
          </div>

          <NodeRunControlActions
            nodeRun={selectedNodeRun}
            workflowId={workflowId}
            runId={runId ?? undefined}
            wsId={wsId}
            size="sm"
            alwaysShow
          />

          {/* Worker */}
          <div className="space-y-1">
            <div className="flex items-center justify-between">
              <span className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider">Executor</span>
              {isWorkerPhase(selectedNodeRun.status) && (
                <span className="flex items-center gap-1 text-[10px] text-blue-600 dark:text-blue-400">
                  <span className="h-1.5 w-1.5 rounded-full bg-blue-500 animate-pulse" />
                  Active
                </span>
              )}
              {isWorkerDone(selectedNodeRun.status) && (
                <span className="flex items-center gap-1 text-[10px] text-emerald-600 dark:text-emerald-400">
                  <svg className="h-3 w-3" viewBox="0 0 15 15" fill="none"><path d="M11.4669 3.72684C11.7558 3.91574 11.8369 4.30308 11.648 4.59198L7.39799 11.092C7.29783 11.2452 7.13556 11.3467 6.95402 11.3699C6.77247 11.3931 6.58989 11.3355 6.45446 11.2124L3.70446 8.71241C3.44905 8.48022 3.43023 8.08494 3.66242 7.82953C3.89461 7.57412 4.28989 7.55529 4.5453 7.78749L6.75292 9.79441L10.6018 3.90792C10.7907 3.61902 11.178 3.53795 11.4669 3.72684Z" fill="currentColor"/></svg>
                  Done
                </span>
              )}
            </div>
            <div className="flex items-center gap-2 text-[11px]">
              <span className="text-muted-foreground w-10 shrink-0">Type</span>
              <span>{selectedNodeRun.worker_type}</span>
            </div>
            <div className="flex items-center gap-2 text-[11px]">
              <span className="text-muted-foreground w-10 shrink-0">Name</span>
              {selectedNodeRun.worker_id ? (
                isWorkerClickable(selectedNodeRun.worker_type, selectedNodeRun.status) ? (
                  <button
                    type="button"
                    className="text-primary underline underline-offset-2 decoration-dotted hover:decoration-solid cursor-pointer text-left"
                    onClick={() => { setTaskLogAgentId(selectedNodeRun.worker_id); setTaskLogOpen(true); }}
                  >
                    {getActorName(workerTypeToActorType(selectedNodeRun.worker_type), selectedNodeRun.worker_id)}
                  </button>
                ) : (
                  <span>
                    {getActorName(workerTypeToActorType(selectedNodeRun.worker_type), selectedNodeRun.worker_id)}
                  </span>
                )
              ) : (
                <span className="text-muted-foreground">—</span>
              )}
            </div>
            {selectedNodeRun.worker_output != null && (
              <div className="text-[11px]">
                <span className="text-muted-foreground">Output</span>
                <pre className="mt-1 p-1.5 rounded bg-muted/50 text-[10px] max-h-24 overflow-y-auto whitespace-pre-wrap break-all">
                  {typeof selectedNodeRun.worker_output === "string"
                    ? selectedNodeRun.worker_output
                    : JSON.stringify(selectedNodeRun.worker_output, null, 2)}
                </pre>
              </div>
            )}
          </div>

          {/* Critic */}
          {(selectedNodeRun.critic_id || selectedNodeRun.critic_comment || selectedNodeRun.critic_output != null) && (
            <div className="space-y-1 pt-2 border-t">
              <div className="flex items-center justify-between">
                <span className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider">Reviewer</span>
                {isCriticPhase(selectedNodeRun.status) && (
                  <span className="flex items-center gap-1 text-[10px] text-purple-600 dark:text-purple-400">
                    <span className="h-1.5 w-1.5 rounded-full bg-purple-500 animate-pulse" />
                    Active
                  </span>
                )}
                {isCriticDone(selectedNodeRun.status) && (
                  <span className="flex items-center gap-1 text-[10px] text-emerald-600 dark:text-emerald-400">
                    <svg className="h-3 w-3" viewBox="0 0 15 15" fill="none"><path d="M11.4669 3.72684C11.7558 3.91574 11.8369 4.30308 11.648 4.59198L7.39799 11.092C7.29783 11.2452 7.13556 11.3467 6.95402 11.3699C6.77247 11.3931 6.58989 11.3355 6.45446 11.2124L3.70446 8.71241C3.44905 8.48022 3.43023 8.08494 3.66242 7.82953C3.89461 7.57412 4.28989 7.55529 4.5453 7.78749L6.75292 9.79441L10.6018 3.90792C10.7907 3.61902 11.178 3.53795 11.4669 3.72684Z" fill="currentColor"/></svg>
                    Done
                  </span>
                )}
              </div>
              {selectedNodeRun.critic_type && (
                <div className="flex items-center gap-2 text-[11px]">
                  <span className="text-muted-foreground w-10 shrink-0">Type</span>
                  <span>{selectedNodeRun.critic_type}</span>
                </div>
              )}
              {selectedNodeRun.critic_id && (
                <div className="flex items-center gap-2 text-[11px]">
                  <span className="text-muted-foreground w-10 shrink-0">Name</span>
                  {isCriticClickable(selectedNodeRun.critic_type, selectedNodeRun.status) ? (
                    <button
                      type="button"
                      className="text-primary underline underline-offset-2 decoration-dotted hover:decoration-solid cursor-pointer text-left"
                      onClick={() => { setTaskLogAgentId(selectedNodeRun.critic_id); setTaskLogOpen(true); }}
                    >
                      {getActorName(workerTypeToActorType(selectedNodeRun.critic_type), selectedNodeRun.critic_id)}
                    </button>
                  ) : (
                    <span>
                      {getActorName(workerTypeToActorType(selectedNodeRun.critic_type), selectedNodeRun.critic_id)}
                    </span>
                  )}
                </div>
              )}
              {selectedNodeRun.critic_comment && (
                <div className="text-[11px]">
                  <span className="text-muted-foreground">Comment</span>
                  <p className="mt-1 p-1.5 rounded bg-muted/50 text-[10px]">{selectedNodeRun.critic_comment}</p>
                </div>
              )}
              {selectedNodeRun.critic_output != null && (
                <div className="text-[11px]">
                  <span className="text-muted-foreground">Output</span>
                  <pre className="mt-1 p-1.5 rounded bg-muted/50 text-[10px] max-h-24 overflow-y-auto whitespace-pre-wrap break-all">
                    {typeof selectedNodeRun.critic_output === "string"
                      ? selectedNodeRun.critic_output
                      : JSON.stringify(selectedNodeRun.critic_output, null, 2)}
                  </pre>
                </div>
              )}
            </div>
          )}

          {/* Meta */}
          <div className="pt-1 border-t flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[10px] text-muted-foreground">
            {selectedNodeRun.started_at && (
              <span>Started: {new Date(selectedNodeRun.started_at).toLocaleString()}</span>
            )}
            {selectedNodeRun.completed_at && (
              <span>Completed: {new Date(selectedNodeRun.completed_at).toLocaleString()}</span>
            )}
            {selectedNodeRun.retry_count > 0 && (
              <span>Retries: {selectedNodeRun.retry_count}</span>
            )}
          </div>
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
