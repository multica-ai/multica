"use client";

import { useState, useEffect, useMemo } from "react";
import { AlertCircle, ArrowUpRight, Clock3, ListTodo, Loader2, Users, UserRoundCog, XCircle } from "lucide-react";
import { toast } from "sonner";
import type { Agent, AgentTask, IssueAssigneeType, UpdateIssueRequest } from "@multica/core/types";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueListOptions } from "@multica/core/issues/queries";
import { useBatchUpdateIssues } from "@multica/core/issues/mutations";
import { useQuery } from "@tanstack/react-query";
import { AssigneePicker } from "@multica/views/issues/components";
import { AppLink } from "@multica/views/navigation";
import { LONG_RUNNING_TASK_MS, getTaskQueueBucket, getTaskQueueDisplay, getTaskReviewFlag } from "../../config";

const REVIEW_ITEM_LIMIT = 3;
const SUMMARY_REFRESH_INTERVAL_MS = 60_000;

function formatTaskTimestamp(task: AgentTask, isRunning: boolean): string {
  if (isRunning && task.started_at) return `Started ${new Date(task.started_at).toLocaleString()}`;
  if (task.status === "dispatched" && task.dispatched_at) return `Dispatched ${new Date(task.dispatched_at).toLocaleString()}`;
  if (task.status === "completed" && task.completed_at) return `Completed ${new Date(task.completed_at).toLocaleString()}`;
  if (task.status === "failed" && task.completed_at) return `Failed ${new Date(task.completed_at).toLocaleString()}`;
  return `Queued ${new Date(task.created_at).toLocaleString()}`;
}

function getReviewSuccessMessage(targetType: IssueAssigneeType, count: number): string {
  const label = targetType === "member" ? "member" : "agent";
  return `Moved ${count} review issue${count > 1 ? "s" : ""} to ${label}`;
}

function getReviewErrorMessage(targetType: IssueAssigneeType): string {
  return targetType === "member"
    ? "Failed to move review issues to a member"
    : "Failed to move review issues to an agent";
}

export function TasksTab({ agent }: { agent: Agent }) {
  const [tasks, setTasks] = useState<AgentTask[]>([]);
  const [loading, setLoading] = useState(true);
  const [currentTime, setCurrentTime] = useState(() => Date.now());
  const [reviewMemberAssignOpen, setReviewMemberAssignOpen] = useState(false);
  const [reviewAgentAssignOpen, setReviewAgentAssignOpen] = useState(false);
  const wsId = useWorkspaceId();
  const { data: issues = [] } = useQuery(issueListOptions(wsId));
  const batchUpdateIssues = useBatchUpdateIssues();

  useEffect(() => {
    setLoading(true);
    api
      .listAgentTasks(agent.id)
      .then(setTasks)
      .catch(() => setTasks([]))
      .finally(() => setLoading(false));
  }, [agent.id]);

  useEffect(() => {
    const interval = window.setInterval(() => setCurrentTime(Date.now()), SUMMARY_REFRESH_INTERVAL_MS);
    return () => window.clearInterval(interval);
  }, []);

  const activeStatuses = ["running", "dispatched", "queued"];
  const sortedTasks = [...tasks].sort((a, b) => {
    const aActive = activeStatuses.indexOf(a.status);
    const bActive = activeStatuses.indexOf(b.status);
    const aIsActive = aActive !== -1;
    const bIsActive = bActive !== -1;
    if (aIsActive && !bIsActive) return -1;
    if (!aIsActive && bIsActive) return 1;
    if (aIsActive && bIsActive) return aActive - bActive;
    return new Date(b.created_at).getTime() - new Date(a.created_at).getTime();
  });

  const issueMap = new Map(issues.map((issue) => [issue.id, issue]));

  const queueSummary = useMemo(() => {
    const counts = { blocked: 0, queued: 0, running: 0, failed: 0 };
    const blockedItems: { issueId: string; id: string; title: string; blockers: number }[] = [];
    const reviewItems: { issueId: string; id: string; title: string; label: string; detail: string }[] = [];
    const reviewCounts = { failed: 0, longRunning: 0 };
    const reviewIssueIds: string[] = [];

    for (const task of tasks) {
      const issue = issueMap.get(task.issue_id);
      const bucket = getTaskQueueBucket(task, issue);

      if (bucket === "blocked") {
        counts.blocked += 1;
        blockedItems.push({
          issueId: task.issue_id,
          id: issue?.identifier ?? task.issue_id.slice(0, 8),
          title: issue?.title ?? "Blocked task",
          blockers: issue?.blocked_by_count ?? 0,
        });
      } else if (bucket === "queued") {
        counts.queued += 1;
      } else if (bucket === "running") {
        counts.running += 1;
      } else if (bucket === "failed") {
        counts.failed += 1;
      }

      const reviewFlag = getTaskReviewFlag(task, currentTime);
      if (!reviewFlag) continue;

      if (reviewFlag.tone === "failed") reviewCounts.failed += 1;
      if (reviewFlag.tone === "long-running") reviewCounts.longRunning += 1;

      reviewIssueIds.push(task.issue_id);
      reviewItems.push({
        issueId: task.issue_id,
        id: issue?.identifier ?? task.issue_id.slice(0, 8),
        title: issue?.title ?? "Assigned issue",
        label: reviewFlag.label,
        detail: reviewFlag.detail,
      });
    }

    return {
      counts,
      blockedItems: blockedItems.slice(0, REVIEW_ITEM_LIMIT),
      reviewItems: reviewItems.slice(0, REVIEW_ITEM_LIMIT),
      reviewCounts,
      reviewIssueIds: Array.from(new Set(reviewIssueIds)),
    };
  }, [currentTime, issueMap, tasks]);

  const reviewThresholdMinutes = Math.floor(LONG_RUNNING_TASK_MS / 60_000);
  const hasReviewItems = queueSummary.reviewCounts.failed > 0 || queueSummary.reviewCounts.longRunning > 0;
  const reviewPanelClass =
    queueSummary.reviewCounts.failed > 0
      ? "border-destructive/20 bg-destructive/5"
      : "border-warning/20 bg-warning/5";

  const handleReviewBatchUpdate = async (
    updates: Partial<UpdateIssueRequest>,
    targetType: IssueAssigneeType,
  ) => {
    const ids = queueSummary.reviewIssueIds;
    if (ids.length === 0) return;

    try {
      await batchUpdateIssues.mutateAsync({ ids, updates });
      toast.success(getReviewSuccessMessage(targetType, ids.length));
      if (targetType === "member") setReviewMemberAssignOpen(false);
      if (targetType === "agent") setReviewAgentAssignOpen(false);
    } catch {
      toast.error(getReviewErrorMessage(targetType));
    }
  };

  if (loading) {
    return (
      <div className="space-y-2">
        {Array.from({ length: 3 }).map((_, i) => (
          <div key={i} className="flex items-center gap-3 rounded-lg border px-4 py-3">
            <Skeleton className="h-4 w-4 rounded shrink-0" />
            <div className="flex-1 space-y-1.5">
              <Skeleton className="h-4 w-1/2" />
              <Skeleton className="h-3 w-1/3" />
            </div>
            <Skeleton className="h-4 w-16" />
          </div>
        ))}
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="space-y-3">
        <div>
          <h3 className="text-sm font-semibold">Task Queue</h3>
          <p className="mt-0.5 text-xs text-muted-foreground">
            Issues assigned to this agent and their execution status.
          </p>
        </div>

        <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-4">
          <div className="rounded-md border px-3 py-2">
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              <AlertCircle className="h-3.5 w-3.5 text-warning" />
              Blocked
            </div>
            <div className="mt-1 text-lg font-semibold tabular-nums">{queueSummary.counts.blocked}</div>
          </div>
          <div className="rounded-md border px-3 py-2">
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              <Clock3 className="h-3.5 w-3.5" />
              Queued
            </div>
            <div className="mt-1 text-lg font-semibold tabular-nums">{queueSummary.counts.queued}</div>
          </div>
          <div className="rounded-md border px-3 py-2">
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              <Loader2 className="h-3.5 w-3.5 text-success" />
              Running
            </div>
            <div className="mt-1 text-lg font-semibold tabular-nums">{queueSummary.counts.running}</div>
          </div>
          <div className="rounded-md border px-3 py-2">
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              <XCircle className="h-3.5 w-3.5 text-destructive" />
              Failed
            </div>
            <div className="mt-1 text-lg font-semibold tabular-nums">{queueSummary.counts.failed}</div>
          </div>
        </div>

        {(queueSummary.counts.blocked > 0 || queueSummary.counts.queued > 0) && (
          <div className="rounded-md border border-warning/20 bg-warning/5 px-3 py-3">
            <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
              Dispatch status
            </div>
            <div className="mt-2 space-y-2 text-sm">
              {queueSummary.counts.blocked > 0 && (
                <div>
                  <span className="font-medium text-foreground">{queueSummary.counts.blocked} blocked</span>{" "}
                  <span className="text-muted-foreground">waiting for unresolved prerequisite issues to reach done or cancelled.</span>
                </div>
              )}
              {queueSummary.blockedItems.length > 0 && (
                <div className="space-y-1 text-xs text-muted-foreground">
                  {queueSummary.blockedItems.map((item) => (
                    <AppLink
                      key={`${item.issueId}-${item.id}`}
                      href={`/issues/${item.issueId}`}
                      className="flex items-start gap-2 rounded-sm px-1 py-1 transition-colors hover:bg-background/60"
                    >
                      <span className="font-mono text-[11px] text-foreground">{item.id}</span>
                      <span className="min-w-0 flex-1 truncate">{item.title}</span>
                      <span className="shrink-0">{item.blockers} blocker{item.blockers === 1 ? "" : "s"}</span>
                      <ArrowUpRight className="mt-0.5 h-3 w-3 shrink-0 opacity-50" />
                    </AppLink>
                  ))}
                </div>
              )}
              {queueSummary.counts.queued > 0 && (
                <div>
                  <span className="font-medium text-foreground">{queueSummary.counts.queued} queued</span>{" "}
                  <span className="text-muted-foreground">waiting for runtime capacity or an earlier run for the same issue to finish.</span>
                </div>
              )}
            </div>
          </div>
        )}

        {hasReviewItems && (
          <div className={`rounded-md border px-3 py-3 ${reviewPanelClass}`}>
            <div className="flex items-start justify-between gap-3">
              <div>
                <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                  Needs review
                </div>
                <div className="mt-1 text-xs text-muted-foreground">
                  Move escalated work directly to a member for manual handling or to another agent for a fresh run.
                </div>
              </div>
              <div className="flex flex-wrap justify-end gap-2">
                <AssigneePicker
                  assigneeType={null}
                  assigneeId={null}
                  onUpdate={(updates) => handleReviewBatchUpdate(updates, "member")}
                  open={reviewMemberAssignOpen}
                  onOpenChange={setReviewMemberAssignOpen}
                  triggerRender={<Button variant="outline" size="sm" />}
                  trigger={
                    <>
                      <Users className="h-3.5 w-3.5" />
                      Move to member
                    </>
                  }
                  align="end"
                  allowedTypes={["member"]}
                  searchPlaceholder="Assign to member..."
                />
                <AssigneePicker
                  assigneeType={null}
                  assigneeId={null}
                  onUpdate={(updates) => handleReviewBatchUpdate(updates, "agent")}
                  open={reviewAgentAssignOpen}
                  onOpenChange={setReviewAgentAssignOpen}
                  triggerRender={<Button variant="outline" size="sm" />}
                  trigger={
                    <>
                      <UserRoundCog className="h-3.5 w-3.5" />
                      Move to agent
                    </>
                  }
                  align="end"
                  allowedTypes={["agent"]}
                  searchPlaceholder="Assign to agent..."
                />
              </div>
            </div>
            <div className="mt-3 space-y-2 text-sm">
              {queueSummary.reviewCounts.failed > 0 && (
                <div>
                  <span className="font-medium text-foreground">{queueSummary.reviewCounts.failed} failed</span>{" "}
                  <span className="text-muted-foreground">should be checked before retrying or reassigning the issue.</span>
                </div>
              )}
              {queueSummary.reviewCounts.longRunning > 0 && (
                <div>
                  <span className="font-medium text-foreground">{queueSummary.reviewCounts.longRunning} long-running</span>{" "}
                  <span className="text-muted-foreground">have been active for {reviewThresholdMinutes}m+ and may need a manual check-in.</span>
                </div>
              )}
              <div className="space-y-1.5 text-xs text-muted-foreground">
                {queueSummary.reviewItems.map((item) => (
                  <AppLink
                    key={`${item.issueId}-${item.label}`}
                    href={`/issues/${item.issueId}`}
                    className="flex items-start gap-2 rounded-sm px-1 py-1 transition-colors hover:bg-background/60"
                  >
                    <span className="font-mono text-[11px] text-foreground">{item.id}</span>
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-foreground">{item.title}</div>
                      <div className="truncate">{item.label}: {item.detail}</div>
                    </div>
                    <ArrowUpRight className="mt-0.5 h-3 w-3 shrink-0 opacity-50" />
                  </AppLink>
                ))}
              </div>
            </div>
          </div>
        )}
      </div>

      {tasks.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-12">
          <ListTodo className="h-8 w-8 text-muted-foreground/40" />
          <p className="mt-3 text-sm text-muted-foreground">No tasks in queue</p>
          <p className="mt-1 text-xs text-muted-foreground">
            Assign an issue to this agent to get started.
          </p>
        </div>
      ) : (
        <div className="space-y-1.5">
          {sortedTasks.map((task) => {
            const issue = issueMap.get(task.issue_id);
            const display = getTaskQueueDisplay(task, issue);
            const reviewFlag = getTaskReviewFlag(task, currentTime);
            const Icon = display.icon;
            const isRunning = task.status === "running";
            const isActive = task.status === "running" || task.status === "dispatched";
            const isFailed = task.status === "failed";
            const isDependencyBlocked = display.tone === "blocked";
            const isHighlighted = isActive || isDependencyBlocked || isFailed;

            return (
              <div
                key={task.id}
                className={`flex items-center gap-3 rounded-lg border px-4 py-3 ${
                  isRunning
                    ? "border-success/40 bg-success/5"
                    : task.status === "dispatched"
                      ? "border-info/40 bg-info/5"
                      : isFailed
                        ? "border-destructive/40 bg-destructive/5"
                        : isDependencyBlocked
                          ? "border-warning/40 bg-warning/5"
                          : ""
                }`}
              >
                <Icon
                  className={`h-4 w-4 shrink-0 ${display.color} ${
                    isRunning ? "animate-spin" : ""
                  }`}
                />
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    {issue && (
                      <span className="shrink-0 text-xs font-mono text-muted-foreground">
                        {issue.identifier}
                      </span>
                    )}
                    <AppLink
                      href={`/issues/${task.issue_id}`}
                      className={`min-w-0 truncate text-sm hover:underline ${isHighlighted ? "font-medium" : ""}`}
                    >
                      {issue?.title ?? `Issue ${task.issue_id.slice(0, 8)}...`}
                    </AppLink>
                  </div>
                  <div className={`mt-0.5 text-xs ${isDependencyBlocked ? "text-warning" : "text-muted-foreground"}`}>
                    {display.detail ?? formatTaskTimestamp(task, isRunning)}
                  </div>
                  {reviewFlag?.tone === "failed" && (
                    <div className="mt-1 line-clamp-2 text-[11px] text-destructive">
                      {reviewFlag.detail}
                    </div>
                  )}
                  {reviewFlag?.tone === "long-running" && (
                    <div className="mt-1 text-[11px] text-warning">
                      {reviewFlag.detail}
                    </div>
                  )}
                </div>
                <span className={`shrink-0 text-xs font-medium ${display.color}`}>
                  {display.label}
                </span>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
