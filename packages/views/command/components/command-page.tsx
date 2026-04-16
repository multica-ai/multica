"use client";

import { useState, useCallback, useRef, useEffect } from "react";
import {
  Send,
  Loader2,
  ExternalLink,
  Sparkles,
  Clock,
  CheckCircle2,
  XCircle,
  PauseCircle,
  Bot,
  ShieldCheck,
  Timer,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { useWorkspacePaths } from "@multica/core/paths";
import { useWorkspaceId } from "@multica/core/hooks";
import { api } from "@multica/core/api";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { workflowRunDetailOptions } from "@multica/core/workflows/queries";
import { useApproveStepRun, useSubmitReview } from "@multica/core/workflows/mutations";
import { AppLink } from "../../navigation";
import { cn } from "@multica/ui/lib/utils";
import { toast } from "sonner";
import type {
  CEOCommandResponse,
  WorkflowStepRun,
  ApprovalDecision,
  ReviewDecision,
} from "@multica/core/types";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function formatDuration(startedAt: string | null, completedAt: string | null): string | null {
  if (!startedAt) return null;
  const start = new Date(startedAt).getTime();
  const end = completedAt ? new Date(completedAt).getTime() : Date.now();
  const ms = end - start;
  if (ms < 1000) return `${ms}ms`;
  const secs = Math.floor(ms / 1000);
  if (secs < 60) return `${secs}s`;
  const mins = Math.floor(secs / 60);
  const remSecs = secs % 60;
  if (mins < 60) return `${mins}m ${remSecs}s`;
  const hours = Math.floor(mins / 60);
  const remMins = mins % 60;
  return `${hours}h ${remMins}m`;
}

const RUN_STATUS_CONFIG: Record<
  string,
  { icon: React.ElementType; color: string; label: string }
> = {
  pending: { icon: Clock, color: "text-muted-foreground", label: "Pending" },
  running: { icon: Loader2, color: "text-blue-500", label: "Running" },
  paused: { icon: PauseCircle, color: "text-yellow-500", label: "Awaiting Approval" },
  planning: { icon: Loader2, color: "text-purple-500", label: "Planning" },
  completed: { icon: CheckCircle2, color: "text-green-500", label: "Completed" },
  failed: { icon: XCircle, color: "text-red-500", label: "Failed" },
  cancelled: { icon: XCircle, color: "text-muted-foreground", label: "Cancelled" },
};

// ---------------------------------------------------------------------------
// Step run detail with duration
// ---------------------------------------------------------------------------

function StepRunCard({
  stepRun,
  onApprove,
  onReview,
}: {
  stepRun: WorkflowStepRun;
  onApprove: (id: string, d: ApprovalDecision) => void;
  onReview: (id: string, d: ReviewDecision) => void;
}) {
  const cfg = RUN_STATUS_CONFIG[stepRun.status] ?? RUN_STATUS_CONFIG.pending!;
  const Icon = cfg.icon;
  const duration = formatDuration(stepRun.started_at, stepRun.completed_at);
  const isRunning = stepRun.status === "running";

  return (
    <div className="space-y-1.5">
      <div className="flex items-center gap-3 rounded-lg border p-3">
        <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-accent">
          {stepRun.step_type === "agent" ? (
            <Bot className="h-4 w-4" />
          ) : stepRun.step_type === "review" ? (
            <ShieldCheck className="h-4 w-4 text-purple-500" />
          ) : stepRun.step_type === "planner" ? (
            <Bot className="h-4 w-4 text-purple-500" />
          ) : (
            <ShieldCheck className="h-4 w-4" />
          )}
        </div>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="text-xs text-muted-foreground">
              {stepRun.step_type === "planner"
                ? "Planner"
                : `Step ${stepRun.step_index + 1}`}
            </span>
            <span className="text-xs font-medium px-1.5 py-0.5 rounded bg-accent">
              {stepRun.step_type === "agent"
                ? "Agent"
                : stepRun.step_type === "review"
                  ? "Review"
                  : stepRun.step_type === "planner"
                    ? "Planner"
                    : "Approval"}
            </span>
          </div>
          <div className="flex items-center gap-1.5 mt-1">
            <Icon
              className={cn(
                "h-3.5 w-3.5",
                cfg.color,
                isRunning && "animate-spin",
              )}
            />
            <span className="text-sm">{cfg.label}</span>
            {stepRun.decision && (
              <span
                className={cn(
                  "text-xs px-1.5 py-0.5 rounded",
                  stepRun.decision === "approved"
                    ? "bg-green-500/10 text-green-600"
                    : stepRun.decision === "rejected"
                      ? "bg-yellow-500/10 text-yellow-600"
                      : "bg-red-500/10 text-red-600",
                )}
              >
                {stepRun.decision}
              </span>
            )}
          </div>
        </div>
        {/* Duration */}
        {duration && (
          <div className="flex items-center gap-1 text-xs text-muted-foreground shrink-0">
            <Timer className="h-3 w-3" />
            <span>{duration}</span>
            {isRunning && <span className="animate-pulse">…</span>}
          </div>
        )}
      </div>
      {/* Approval buttons */}
      {stepRun.step_type === "approval" && stepRun.status === "paused" && (
        <div className="flex gap-2 pl-11">
          <Button size="sm" variant="outline" className="h-7 text-xs text-green-600"
            onClick={() => onApprove(stepRun.id, "approved")}>Approve</Button>
          <Button size="sm" variant="outline" className="h-7 text-xs text-yellow-600"
            onClick={() => onApprove(stepRun.id, "rejected")}>Reject</Button>
          <Button size="sm" variant="outline" className="h-7 text-xs text-red-600"
            onClick={() => onApprove(stepRun.id, "stopped")}>Stop</Button>
        </div>
      )}
      {stepRun.step_type === "review" && stepRun.status === "paused" && (
        <div className="flex gap-2 pl-11">
          <Button size="sm" variant="outline" className="h-7 text-xs text-green-600"
            onClick={() => onReview(stepRun.id, "approved")}>Approve</Button>
          <Button size="sm" variant="outline" className="h-7 text-xs text-yellow-600"
            onClick={() => onReview(stepRun.id, "rejected")}>Reject</Button>
          <Button size="sm" variant="outline" className="h-7 text-xs text-red-600"
            onClick={() => onReview(stepRun.id, "stopped")}>Stop</Button>
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Run detail panel (shows step runs with durations)
// ---------------------------------------------------------------------------

function RunDetailPanel({ runId }: { runId: string }) {
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { data: runDetail, isLoading } = useQuery({
    ...workflowRunDetailOptions(wsId, runId),
    refetchInterval: 5000,
  });
  const approveStep = useApproveStepRun();
  const submitReview = useSubmitReview();

  const handleApproval = useCallback(
    (stepRunId: string, decision: ApprovalDecision) => {
      approveStep.mutate(
        { stepRunId, decision },
        {
          onSuccess: () => toast.success("Decision submitted"),
          onError: () => toast.error("Failed to submit decision"),
        },
      );
    },
    [approveStep],
  );

  const handleReview = useCallback(
    (stepRunId: string, decision: ReviewDecision) => {
      submitReview.mutate(
        { stepRunId, decision },
        {
          onSuccess: () => toast.success("Review decision submitted"),
          onError: () => toast.error("Failed to submit review"),
        },
      );
    },
    [submitReview],
  );

  if (isLoading) {
    return (
      <div className="flex items-center gap-2 p-4 text-sm text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        Loading run details…
      </div>
    );
  }

  if (!runDetail) return null;

  // API returns { run: WorkflowRun, step_runs: WorkflowStepRun[] }
  const run = (runDetail as any).run ?? runDetail;
  const stepRuns: WorkflowStepRun[] =
    (runDetail as any).step_runs ?? (runDetail as any).stepRuns ?? [];

  const runCfg = RUN_STATUS_CONFIG[run.status] ?? RUN_STATUS_CONFIG.pending!;
  const RunIcon = runCfg.icon;
  const totalDuration = formatDuration(run.started_at, run.completed_at);

  return (
    <div className="space-y-3 p-4 border-t">
      <div className="flex items-center gap-2">
        <RunIcon
          className={cn(
            "h-4 w-4",
            runCfg.color,
            (run.status === "running" || run.status === "planning") &&
              "animate-spin",
          )}
        />
        <span className="text-sm font-medium">{runCfg.label}</span>
        {totalDuration && (
          <span className="flex items-center gap-1 text-xs text-muted-foreground ml-auto">
            <Timer className="h-3 w-3" />
            Total: {totalDuration}
          </span>
        )}
      </div>
      {run.issue_id && (
        <AppLink
          href={p.issueDetail(run.issue_id)}
          className="text-xs text-brand hover:underline flex items-center gap-1"
        >
          View Issue <ExternalLink className="h-3 w-3" />
        </AppLink>
      )}
      {stepRuns.map((sr: WorkflowStepRun) => (
        <StepRunCard
          key={sr.id}
          stepRun={sr}
          onApprove={handleApproval}
          onReview={handleReview}
        />
      ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Chat entry types
// ---------------------------------------------------------------------------

interface ChatEntry {
  id: string;
  message: string;
  skipReview: boolean;
  status: "sending" | "success" | "error";
  result?: CEOCommandResponse;
  error?: string;
  timestamp: Date;
}

// ---------------------------------------------------------------------------
// Main Component
// ---------------------------------------------------------------------------

export function CommandPage() {
  const p = useWorkspacePaths();
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();

  const [input, setInput] = useState("");
  const [entries, setEntries] = useState<ChatEntry[]>([]);
  const [isSending, setIsSending] = useState(false);
  const [selectedRunId, setSelectedRunId] = useState<string | null>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const listRef = useRef<HTMLDivElement>(null);

  // Load CEO command history
  const { data: history } = useQuery({
    queryKey: ["ceo-commands", wsId],
    queryFn: () => api.listCEOCommands(),
    select: (d) => d.commands,
    refetchInterval: 15000,
  });

  useEffect(() => {
    if (listRef.current) {
      listRef.current.scrollTop = listRef.current.scrollHeight;
    }
  }, [entries]);

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  const handleSend = useCallback(
    async (skipReview = false) => {
      const message = input.trim();
      if (!message || isSending) return;

      const id = `cmd-${Date.now()}`;
      const entry: ChatEntry = {
        id,
        message,
        skipReview,
        status: "sending",
        timestamp: new Date(),
      };

      setEntries((prev) => [...prev, entry]);
      setInput("");
      setIsSending(true);

      try {
        const result = await api.sendCEOCommand(message, skipReview);
        setEntries((prev) =>
          prev.map((e) =>
            e.id === id ? { ...e, status: "success", result } : e,
          ),
        );
        setSelectedRunId(result.workflow_run_id);
        queryClient.invalidateQueries({ queryKey: ["ceo-commands", wsId] });
        toast.success(
          skipReview
            ? "Command dispatched (no review)"
            : "Command dispatched to CEO agent",
        );
      } catch (err: any) {
        const errorMsg = err?.message || "Failed to send command";
        setEntries((prev) =>
          prev.map((e) =>
            e.id === id ? { ...e, status: "error", error: errorMsg } : e,
          ),
        );
        toast.error(errorMsg);
      } finally {
        setIsSending(false);
        inputRef.current?.focus();
      }
    },
    [input, isSending, queryClient, wsId],
  );

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === "Enter" && !e.shiftKey) {
        e.preventDefault();
        handleSend(false);
      }
    },
    [handleSend],
  );

  return (
    <div className="flex h-full">
      {/* Left sidebar: CEO Command History */}
      <div className="w-64 shrink-0 border-r flex flex-col overflow-hidden">
        <div className="flex items-center gap-2 px-4 py-3 border-b">
          <Sparkles className="size-4 text-brand" />
          <span className="text-sm font-medium">History</span>
          <span className="ml-auto text-xs text-muted-foreground">
            {history?.length ?? 0}
          </span>
        </div>
        <div className="flex-1 overflow-y-auto">
          {(!history || history.length === 0) && entries.length === 0 && (
            <div className="p-4 text-xs text-muted-foreground text-center">
              No commands yet
            </div>
          )}
          {(history ?? []).map((cmd) => {
            const statusCfg = cmd.run_status
              ? RUN_STATUS_CONFIG[cmd.run_status]
              : null;
            const StatusIcon = statusCfg?.icon ?? Clock;
            const isActive = selectedRunId === cmd.run_id;
            return (
              <button
                key={cmd.workflow_id}
                type="button"
                className={cn(
                  "w-full text-left px-4 py-2.5 border-b text-xs hover:bg-accent/40 transition-colors",
                  isActive && "bg-accent/60",
                )}
                onClick={() => cmd.run_id && setSelectedRunId(cmd.run_id)}
              >
                <div className="flex items-center gap-1.5 mb-1">
                  <StatusIcon
                    className={cn(
                      "h-3 w-3 shrink-0",
                      statusCfg?.color ?? "text-muted-foreground",
                      (cmd.run_status === "running" ||
                        cmd.run_status === "planning") &&
                        "animate-spin",
                    )}
                  />
                  <span className="truncate font-medium">
                    {cmd.name.replace(/^\[CEO\]\s*/, "")}
                  </span>
                </div>
                <span className="text-muted-foreground">
                  {new Date(cmd.created_at).toLocaleString()}
                </span>
              </button>
            );
          })}
        </div>
      </div>

      {/* Main chat area */}
      <div className="flex-1 flex flex-col min-w-0">
        <header className="flex items-center gap-3 border-b px-6 py-4">
          <div className="flex items-center gap-2">
            <Sparkles className="size-5 text-brand" />
            <h1 className="text-lg font-semibold">CEO Command</h1>
          </div>
          <span className="text-sm text-muted-foreground">
            Send instructions and let the CEO agent plan &amp; delegate
          </span>
        </header>

        <div ref={listRef} className="flex-1 overflow-y-auto px-6 py-4 space-y-4">
          {entries.length === 0 && !selectedRunId && (
            <div className="flex flex-col items-center justify-center h-full text-center text-muted-foreground gap-3">
              <Sparkles className="size-10 opacity-30" />
              <p className="text-lg font-medium">Tell CEO what you need</p>
              <p className="text-sm max-w-md">
                Type a command below. The CEO agent will create a plan and
                assign tasks to the appropriate agents automatically.
              </p>
            </div>
          )}

          {entries.map((entry) => (
            <div key={entry.id} className="space-y-2">
              <div className="flex justify-end">
                <div className="max-w-[80%] rounded-xl bg-brand/10 px-4 py-2.5 text-sm">
                  <p className="whitespace-pre-wrap">{entry.message}</p>
                  <div className="mt-1 flex items-center gap-2 text-[11px] text-muted-foreground">
                    <span>{entry.timestamp.toLocaleTimeString()}</span>
                    {entry.skipReview && (
                      <span className="text-orange-500 font-medium">No Review</span>
                    )}
                  </div>
                </div>
              </div>
              <div className="flex justify-start">
                <div className="max-w-[80%] rounded-xl ring-1 ring-border px-4 py-2.5 text-sm">
                  {entry.status === "sending" && (
                    <div className="flex items-center gap-2 text-muted-foreground">
                      <Loader2 className="size-4 animate-spin" />
                      <span>Dispatching to CEO agent…</span>
                    </div>
                  )}
                  {entry.status === "error" && (
                    <p className="text-destructive">{entry.error}</p>
                  )}
                  {entry.status === "success" && entry.result && (
                    <div className="space-y-2">
                      <p className="text-green-600 dark:text-green-400 font-medium">
                        ✓ Command dispatched — CEO is planning
                      </p>
                      <div className="grid gap-1 text-xs text-muted-foreground">
                        {entry.result.issue_id && (
                          <div className="flex items-center gap-1">
                            <span>Issue:</span>
                            <AppLink
                              href={p.issueDetail(entry.result.issue_id)}
                              className="inline-flex items-center gap-0.5 text-brand hover:underline"
                            >
                              {entry.result.issue_id.slice(0, 8)}…
                              <ExternalLink className="size-3" />
                            </AppLink>
                          </div>
                        )}
                        <button
                          type="button"
                          className="flex items-center gap-1 text-brand hover:underline"
                          onClick={() =>
                            setSelectedRunId(entry.result!.workflow_run_id)
                          }
                        >
                          View Run Detail
                          <ExternalLink className="size-3" />
                        </button>
                      </div>
                    </div>
                  )}
                </div>
              </div>
            </div>
          ))}

          {selectedRunId && (
            <div className="rounded-lg border bg-card">
              <div className="flex items-center justify-between px-4 py-2 border-b">
                <span className="text-sm font-medium">Run Detail</span>
                <button
                  type="button"
                  className="text-xs text-muted-foreground hover:text-foreground"
                  onClick={() => setSelectedRunId(null)}
                >
                  Close
                </button>
              </div>
              <RunDetailPanel runId={selectedRunId} />
            </div>
          )}
        </div>

        {/* Input area with dual send buttons */}
        <div className="border-t px-6 py-4">
          <div className="flex items-end gap-2">
            <textarea
              ref={inputRef}
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder="Tell the CEO what to do…"
              rows={1}
              className="flex-1 resize-none rounded-lg border bg-background px-3 py-2.5 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
              style={{ minHeight: 42, maxHeight: 160 }}
              onInput={(e) => {
                const target = e.target as HTMLTextAreaElement;
                target.style.height = "auto";
                target.style.height = `${Math.min(target.scrollHeight, 160)}px`;
              }}
            />
            {/* Normal send (with review) */}
            <Button
              size="icon"
              disabled={!input.trim() || isSending}
              onClick={() => handleSend(false)}
              className="shrink-0"
              title="Send (with review)"
            >
              {isSending ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <Send className="size-4" />
              )}
            </Button>
            {/* No-review send */}
            <Button
              size="icon"
              variant="outline"
              disabled={!input.trim() || isSending}
              onClick={() => handleSend(true)}
              className="shrink-0 border-orange-400 text-orange-500 hover:bg-orange-50 dark:hover:bg-orange-950"
              title="Send without review (skip all approval steps)"
            >
              {isSending ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <span className="text-xs font-bold">⚡</span>
              )}
            </Button>
          </div>
          <p className="mt-1.5 text-[11px] text-muted-foreground">
            Enter = Send with review · ⚡ = Send without review ·
            Shift+Enter = New line
          </p>
        </div>
      </div>
    </div>
  );
}
