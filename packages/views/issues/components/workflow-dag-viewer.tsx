"use client";

import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { DAGCanvas } from "../../workflows/components";
import {
  workflowDetailOptions,
  workflowNodesOptions,
  workflowEdgesOptions,
  workflowNodeRunsOptions,
  useCancelWorkflowRun,
} from "@multica/core/workflows/queries";
import { taskMessagesOptions } from "@multica/core/chat/queries";
import { useActorName } from "@multica/core/workspace/hooks";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogHeader } from "@multica/ui/components/ui/dialog";
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
  return { label: "Pending", variant: "outline" as const };
}

function workerTypeToActorType(t: string): string {
  if (t === "human") return "member";
  if (t === "agent") return "agent";
  if (t === "squad") return "squad";
  return "member";
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
  const cancelRunMutation = useCancelWorkflowRun(wsId);
  const { getActorName } = useActorName();

  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [taskLogOpen, setTaskLogOpen] = useState(false);
  const [taskLogTarget, setTaskLogTarget] = useState<"executor" | "reviewer">("executor");

  const nodeStatusColors: Record<string, string> = {};
  const nodeStatuses: Record<string, { status: string; isRunning: boolean }> = {};
  const runningSet = new Set(["format_checking", "working", "critic_reviewing"]);
  for (const nr of nodeRuns) {
    nodeStatusColors[nr.workflow_node_id] = getStatusColor(nr.status);
    nodeStatuses[nr.workflow_node_id] = {
      status: getStatusLabel(nr.status),
      isRunning: runningSet.has(nr.status),
    };
  }

  const totalCount = nodes.length;
  const summary = getRunSummary(nodeRuns);

  const handleCancel = async () => {
    if (!runId || !confirm("Cancel this workflow run? All active sub-issues will stop.")) return;
    try {
      await cancelRunMutation.mutateAsync({ workflowId, runId });
    } catch {
      // silent
    }
  };

  const isRunning = summary.label === "In Progress" || summary.label === "Pending";

  const selectedNodeRun = selectedNodeId
    ? nodeRuns.find((nr) => nr.workflow_node_id === selectedNodeId) ?? null
    : null;
  const selectedNode = selectedNodeId
    ? nodes.find((n) => n.id === selectedNodeId) ?? null
    : null;

  const taskId = selectedNodeRun
    ? (taskLogTarget === "reviewer"
        ? (selectedNodeRun.critic_agent_task_id || selectedNodeRun.agent_task_id)
        : (selectedNodeRun.worker_agent_task_id || selectedNodeRun.agent_task_id)) ?? ""
    : "";
  const { data: taskMessages = [] } = useQuery({
    ...taskMessagesOptions(taskId),
    enabled: !!taskId,
  });

  const logRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight;
  }, [taskMessages]);

  const taskAgentName = selectedNodeRun
    ? (() => {
        const isCritic = taskLogTarget === "reviewer";
        const type = workerTypeToActorType(
          isCritic ? selectedNodeRun.critic_type : selectedNodeRun.worker_type
        );
        const id = isCritic ? selectedNodeRun.critic_id : selectedNodeRun.worker_id;
        return id ? getActorName(type, id) : null;
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
        {isRunning && runId && (
          <Button
            size="sm"
            variant="ghost"
            className="h-6 text-xs text-destructive hover:text-destructive"
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

      <div className={cn("h-[400px] overflow-hidden rounded-lg border bg-card", !runId && "opacity-60")}>
        <DAGCanvas
          nodes={nodes}
          edges={edges}
          nodeStatusColors={nodeStatusColors}
          nodeStatuses={nodeStatuses}
          onNodeClick={(id) => setSelectedNodeId(id === selectedNodeId ? null : id)}
          initialScale={2}
        />
      </div>

      {/* Selected node run detail panel */}
      {selectedNodeRun && selectedNode && (
        <div className="mt-3 rounded-lg border bg-card p-3 space-y-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <span className="text-sm font-medium">{selectedNode.title}</span>
              <Badge variant="secondary" className="text-[10px] px-1.5 h-4">
                {getStatusLabel(selectedNodeRun.status)}
              </Badge>
            </div>
            <Button
              variant="ghost"
              size="icon"
              className="h-5 w-5"
              onClick={() => setSelectedNodeId(null)}
            >
              <svg width="12" height="12" viewBox="0 0 15 15" fill="none">
                <path d="M11.7816 4.03157C12.0062 3.80702 12.0062 3.44295 11.7816 3.2184C11.5571 2.99385 11.193 2.99385 10.9685 3.2184L7.50005 6.68682L4.03164 3.2184C3.80708 2.99385 3.44301 2.99385 3.21846 3.2184C2.99391 3.44295 2.99391 3.80702 3.21846 4.03157L6.68688 7.49999L3.21846 10.9684C2.99391 11.193 2.99391 11.557 3.21846 11.7816C3.44301 12.0061 3.80708 12.0061 4.03164 11.7816L7.50005 8.31316L10.9685 11.7816C11.193 12.0061 11.5571 12.0061 11.7816 11.7816C12.0062 11.557 12.0062 11.193 11.7816 10.9684L8.31322 7.49999L11.7816 4.03157Z" fill="currentColor" />
              </svg>
            </Button>
          </div>

          {/* Worker section */}
          <div className="space-y-1.5 pt-2 border-t">
            <div className="flex items-center justify-between">
              <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Executor</span>
              {isWorkerPhase(selectedNodeRun.status) && (
                <Badge variant="secondary" className="text-[10px] px-1 h-3.5 animate-pulse">Active</Badge>
              )}
            </div>
            <div className="flex items-center gap-2 text-xs">
              <span className="text-muted-foreground w-10 shrink-0">Type</span>
              <span>{selectedNodeRun.worker_type}</span>
            </div>
            <div className="flex items-center gap-2 text-xs">
              <span className="text-muted-foreground w-10 shrink-0">Name</span>
              {selectedNodeRun.worker_id ? (
                <button
                  type="button"
                  className="text-primary hover:underline text-left"
                  onClick={() => { setTaskLogTarget("executor"); setTaskLogOpen(true); }}
                >
                  {getActorName(workerTypeToActorType(selectedNodeRun.worker_type), selectedNodeRun.worker_id)}
                </button>
              ) : (
                <span>—</span>
              )}
            </div>
            {selectedNodeRun.worker_output != null && (
              <div className="text-xs">
                <span className="text-muted-foreground">Output</span>
                <pre className="mt-1 p-2 rounded bg-muted/50 text-[11px] max-h-32 overflow-y-auto whitespace-pre-wrap break-all">
                  {typeof selectedNodeRun.worker_output === "string"
                    ? selectedNodeRun.worker_output
                    : JSON.stringify(selectedNodeRun.worker_output, null, 2)}
                </pre>
              </div>
            )}
          </div>

          {/* Critic section */}
          <div className="space-y-1.5 pt-2 border-t">
            <div className="flex items-center justify-between">
              <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Reviewer</span>
              {isCriticPhase(selectedNodeRun.status) && (
                <Badge variant="secondary" className="text-[10px] px-1 h-3.5 animate-pulse">Active</Badge>
              )}
            </div>
            <div className="flex items-center gap-2 text-xs">
              <span className="text-muted-foreground w-10 shrink-0">Type</span>
              <span>{selectedNodeRun.critic_type}</span>
            </div>
            <div className="flex items-center gap-2 text-xs">
              <span className="text-muted-foreground w-10 shrink-0">Name</span>
              {selectedNodeRun.critic_id ? (
                <button
                  type="button"
                  className="text-primary hover:underline text-left"
                  onClick={() => { setTaskLogTarget("reviewer"); setTaskLogOpen(true); }}
                >
                  {getActorName(workerTypeToActorType(selectedNodeRun.critic_type), selectedNodeRun.critic_id)}
                </button>
              ) : (
                <span>—</span>
              )}
            </div>
            {selectedNodeRun.critic_comment && (
              <div className="text-xs">
                <span className="text-muted-foreground">Comment</span>
                <p className="mt-1 p-2 rounded bg-muted/50 text-[11px]">{selectedNodeRun.critic_comment}</p>
              </div>
            )}
            {selectedNodeRun.critic_output != null && (
              <div className="text-xs">
                <span className="text-muted-foreground">Output</span>
                <pre className="mt-1 p-2 rounded bg-muted/50 text-[11px] max-h-32 overflow-y-auto whitespace-pre-wrap break-all">
                  {typeof selectedNodeRun.critic_output === "string"
                    ? selectedNodeRun.critic_output
                    : JSON.stringify(selectedNodeRun.critic_output, null, 2)}
                </pre>
              </div>
            )}
          </div>

          {/* Meta */}
          <div className="pt-2 border-t flex items-center gap-4 text-[10px] text-muted-foreground">
            {selectedNodeRun.started_at && (
              <span>Started: {new Date(selectedNodeRun.started_at).toLocaleString()}</span>
            )}
            {selectedNodeRun.completed_at && (
              <span>Completed: {new Date(selectedNodeRun.completed_at).toLocaleString()}</span>
            )}
            {selectedNodeRun.retry_count > 0 && (
              <span>Retries: {selectedNodeRun.retry_count}</span>
            )}
            {selectedNodeRun.agent_task_id && (
              <span className="font-mono">Task: {selectedNodeRun.agent_task_id.slice(0, 8)}...</span>
            )}
          </div>
        </div>
      )}

      {/* Task execution log dialog */}
      <Dialog open={taskLogOpen} onOpenChange={setTaskLogOpen}>
        <DialogContent className="sm:max-w-lg max-h-[80vh] flex flex-col">
          <DialogHeader>
            <span className="text-sm font-medium">
              Task Log{taskAgentName ? ` — ${taskAgentName}` : ""}
            </span>
          </DialogHeader>
          {taskMessages.length > 0 ? (
            <div ref={logRef} className="flex-1 overflow-y-auto space-y-1 min-h-0">
              {taskMessages.map((msg, i) => (
                <div
                  key={i}
                  className={cn(
                    "text-[11px] px-2 py-1 rounded",
                    msg.type === "error" && "bg-red-50 text-red-700 dark:bg-red-950 dark:text-red-300",
                    msg.type === "thinking" && "bg-muted/30 text-muted-foreground italic",
                    msg.type === "tool_use" && "bg-blue-50 text-blue-700 dark:bg-blue-950 dark:text-blue-300",
                    msg.type === "tool_result" && "bg-muted/20 text-muted-foreground",
                    msg.type === "text" && "text-foreground",
                  )}
                >
                  {msg.type === "tool_use" && msg.tool && (
                    <span className="font-medium">Tool: {msg.tool}</span>
                  )}
                  {msg.content && <p className="whitespace-pre-wrap break-all">{msg.content}</p>}
                  {msg.type === "tool_result" && msg.output && (
                    <p className="whitespace-pre-wrap break-all text-[10px]">{msg.output}</p>
                  )}
                </div>
              ))}
            </div>
          ) : (
            <p className="text-xs text-muted-foreground">Task is queued — no execution messages yet. The agent may be offline or busy.</p>
          )}
        </DialogContent>
      </Dialog>

      {runId && nodeRuns.length > 0 && (
        <div className="mt-3 space-y-1">
          {nodeRuns.map((nr) => {
            const node = nodes.find((n) => n.id === nr.workflow_node_id);
            return (
              <button
                key={nr.id}
                type="button"
                className={cn(
                  "w-full flex items-center gap-2 text-xs px-2 py-1 rounded hover:bg-accent/40 transition-colors",
                  nr.workflow_node_id === selectedNodeId && "bg-accent/60"
                )}
                onClick={() => setSelectedNodeId(nr.workflow_node_id === selectedNodeId ? null : nr.workflow_node_id)}
              >
                <span
                  className="w-2 h-2 rounded-full shrink-0"
                  style={{ backgroundColor: getStatusColor(nr.status) }}
                />
                <span className="w-28 truncate text-muted-foreground text-left">
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
              </button>
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
