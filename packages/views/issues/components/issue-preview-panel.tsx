"use client";

import { useCallback, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import type {
  IssueExecutionSummary,
  IssuePriority,
  IssueStatus,
  TimelineEntry,
  UpdateIssueRequest,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { ScrollArea } from "@multica/ui/components/ui/scroll-area";
import { Separator } from "@multica/ui/components/ui/separator";
import { cn } from "@multica/ui/lib/utils";
import {
  ChevronLeft,
  ChevronRight,
  ExternalLink,
  MessageSquare,
  PanelRightClose,
} from "lucide-react";
import { toast } from "sonner";
import { useActorName } from "@multica/core/workspace/hooks";
import { timeAgo } from "@multica/core/utils";
import { ReadonlyContent, TitleEditor } from "../../editor";
import { CommentInput } from "./comment-input";
import { AgentLiveCard } from "./agent-live-card";
import { IssueExecutionBanner } from "./issue-execution";
import { StatusIcon } from "./status-icon";
import { PriorityIcon } from "./priority-icon";
import {
  AssigneePicker,
  DueDatePicker,
  PriorityPicker,
  StatusPicker,
} from "./pickers";
import { useIssueTimeline } from "../hooks/use-issue-timeline";

function shortDate(date: string | null): string {
  if (!date) return "No due date";
  return new Date(date).toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
  });
}

function describeActivity(
  entry: TimelineEntry,
  getActorName: (type: string, id: string) => string,
) {
  if (entry.type === "comment") {
    return {
      title: getActorName(entry.actor_type, entry.actor_id),
      body: entry.content ?? "",
      kind: "comment" as const,
    };
  }

  const details = (entry.details ?? {}) as Record<string, string>;
  switch (entry.action) {
    case "created":
      return {
        title: getActorName(entry.actor_type, entry.actor_id),
        body: "created this issue",
        kind: "activity" as const,
      };
    case "status_changed":
      return {
        title: getActorName(entry.actor_type, entry.actor_id),
        body: `changed status to ${details.to ?? "unknown"}`,
        kind: "activity" as const,
      };
    case "priority_changed":
      return {
        title: getActorName(entry.actor_type, entry.actor_id),
        body: `changed priority to ${details.to ?? "unknown"}`,
        kind: "activity" as const,
      };
    case "assignee_changed":
      return {
        title: getActorName(entry.actor_type, entry.actor_id),
        body: "updated the assignee",
        kind: "activity" as const,
      };
    case "task_failed":
      return {
        title: getActorName(entry.actor_type, entry.actor_id),
        body: "last execution failed",
        kind: "activity" as const,
      };
    case "task_completed":
      return {
        title: getActorName(entry.actor_type, entry.actor_id),
        body: "completed the latest execution",
        kind: "activity" as const,
      };
    default:
      return {
        title: getActorName(entry.actor_type, entry.actor_id),
        body: (entry.action ?? "updated issue").replaceAll("_", " "),
        kind: "activity" as const,
      };
  }
}

function PreviewProperty({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex items-center gap-2 rounded-md border px-2.5 py-2">
      <span className="w-20 shrink-0 text-xs text-muted-foreground">
        {label}
      </span>
      <div className="min-w-0 flex-1 text-sm">{children}</div>
    </div>
  );
}

function describeExecutionSnapshot(summary: IssueExecutionSummary) {
  switch (summary.state) {
    case "queued":
      return {
        title: "Queued for execution",
        detail:
          summary.queued_count > 1
            ? `${summary.queued_count} pending runs in this issue`
            : "Waiting for the next available execution slot",
      };
    case "running":
      return {
        title: "Execution in progress",
        detail:
          summary.running_count > 1
            ? `${summary.running_count} active runs in progress`
            : "Streaming the current run below",
      };
    case "failed":
      return {
        title: "Latest execution failed",
        detail: summary.latest_completed_at
          ? `Finished ${timeAgo(summary.latest_completed_at)}`
          : "Review the latest failure before retrying",
      };
    case "completed":
      return {
        title: "Latest execution completed",
        detail: summary.latest_completed_at
          ? `Finished ${timeAgo(summary.latest_completed_at)}`
          : "Most recent run completed successfully",
      };
    default:
      return {
        title: "No recent execution",
        detail: "Assign an agent or leave a comment to trigger a run.",
      };
  }
}

export function IssuePreviewPanel({
  issueId,
  summary,
  notInCurrentView,
  canGoPrev,
  canGoNext,
  onGoPrev,
  onGoNext,
  onClose,
  onExpand,
}: {
  issueId: string;
  summary: IssueExecutionSummary | undefined;
  notInCurrentView: boolean;
  canGoPrev: boolean;
  canGoNext: boolean;
  onGoPrev: () => void;
  onGoNext: () => void;
  onClose: () => void;
  onExpand: () => void;
}) {
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id);
  const { getActorName } = useActorName();
  const updateIssue = useUpdateIssue();
  const { data: issue, isLoading } = useQuery({
    ...issueDetailOptions(wsId, issueId),
    enabled: !!issueId,
  });
  const { timeline, submitComment } = useIssueTimeline(issueId, userId);

  const handleUpdate = useCallback(
    (updates: Partial<UpdateIssueRequest>) => {
      updateIssue.mutate(
        { id: issueId, ...updates },
        { onError: () => toast.error("Failed to update issue") },
      );
    },
    [issueId, updateIssue],
  );

  const recentEntries = useMemo(() => {
    return [...timeline].reverse().slice(0, 8);
  }, [timeline]);
  const executionSnapshot = useMemo(
    () => (summary ? describeExecutionSnapshot(summary) : null),
    [summary],
  );

  if (isLoading) {
    return (
      <div className="flex h-full min-h-0 w-full flex-col">
        <div className="flex h-12 items-center justify-between border-b px-4">
          <Skeleton className="h-4 w-24" />
          <div className="flex items-center gap-2">
            <Skeleton className="h-8 w-8 rounded-md" />
            <Skeleton className="h-8 w-8 rounded-md" />
          </div>
        </div>
        <div className="space-y-3 p-4">
          <Skeleton className="h-8 w-2/3" />
          <Skeleton className="h-16 w-full rounded-lg" />
          <Skeleton className="h-24 w-full rounded-lg" />
        </div>
      </div>
    );
  }

  if (!issue) {
    return (
      <div className="flex h-full min-h-0 w-full flex-col items-center justify-center gap-3 p-6 text-center">
        <p className="text-sm text-muted-foreground">
          This issue is no longer available.
        </p>
        <Button variant="outline" size="sm" onClick={onClose}>
          Close preview
        </Button>
      </div>
    );
  }

  return (
    <div className="flex h-full min-h-0 w-full flex-col bg-background">
      <div className="flex h-12 items-center justify-between border-b px-4">
        <div className="min-w-0">
          <div className="truncate text-xs text-muted-foreground">
            {issue.identifier}
          </div>
        </div>
        <div className="flex items-center gap-1">
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={onGoPrev}
            disabled={!canGoPrev || notInCurrentView}
            title="Previous issue"
          >
            <ChevronLeft className="h-4 w-4" />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={onGoNext}
            disabled={!canGoNext || notInCurrentView}
            title="Next issue"
          >
            <ChevronRight className="h-4 w-4" />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={onExpand}
            title="Open full detail"
          >
            <ExternalLink className="h-4 w-4" />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={onClose}
            title="Close preview"
          >
            <PanelRightClose className="h-4 w-4" />
          </Button>
        </div>
      </div>

      <ScrollArea className="min-h-0 flex-1 overflow-hidden">
        <div className="space-y-4 p-4">
          {notInCurrentView && (
            <div className="rounded-lg border border-warning/30 bg-warning/10 px-3 py-2 text-xs text-warning">
              Not in current view. Change filters or status to navigate within this lane.
            </div>
          )}

          <TitleEditor
            key={`preview-title-${issue.id}`}
            defaultValue={issue.title}
            placeholder="Issue title"
            className="w-full text-xl font-semibold leading-snug"
            onBlur={(value) => {
              const trimmed = value.trim();
              if (trimmed && trimmed !== issue.title) {
                handleUpdate({ title: trimmed });
              }
            }}
          />

          <IssueExecutionBanner summary={summary} />

          <div className="grid gap-2">
            <PreviewProperty label="Status">
              <StatusPicker
                status={issue.status}
                onUpdate={handleUpdate}
                align="start"
              />
            </PreviewProperty>
            <PreviewProperty label="Priority">
              <PriorityPicker
                priority={issue.priority}
                onUpdate={handleUpdate}
                align="start"
              />
            </PreviewProperty>
            <PreviewProperty label="Assignee">
              <AssigneePicker
                assigneeType={issue.assignee_type}
                assigneeId={issue.assignee_id}
                onUpdate={handleUpdate}
                align="start"
              />
            </PreviewProperty>
            <PreviewProperty label="Due date">
              <DueDatePicker dueDate={issue.due_date} onUpdate={handleUpdate} />
            </PreviewProperty>
          </div>

          {issue.description && (
            <div className="space-y-2">
              <div className="text-xs font-medium text-muted-foreground">
                Description
              </div>
              <div className="rounded-lg border px-3 py-3">
                <ReadonlyContent content={issue.description} />
              </div>
            </div>
          )}

          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <div className="text-xs font-medium text-muted-foreground">
                Quick comment
              </div>
              <Button
                variant="ghost"
                size="sm"
                className="h-7 text-xs"
                onClick={onExpand}
              >
                View full detail
              </Button>
            </div>
            <CommentInput issueId={issueId} onSubmit={submitComment} />
          </div>

          <div className="space-y-2">
            <div className="text-xs font-medium text-muted-foreground">
              Current execution
            </div>
            <AgentLiveCard issueId={issueId} />
            {executionSnapshot ? (
              <div className="rounded-lg border px-3 py-3">
                <div className="text-sm font-medium">
                  {executionSnapshot.title}
                </div>
                <p className="mt-1 text-sm text-muted-foreground">
                  {executionSnapshot.detail}
                </p>
                {summary?.latest_trigger_excerpt && (
                  <p className="mt-2 line-clamp-2 text-xs text-muted-foreground">
                    Trigger: {summary.latest_trigger_excerpt}
                  </p>
                )}
                {summary?.latest_error && (
                  <p className="mt-2 line-clamp-3 text-xs text-destructive">
                    {summary.latest_error}
                  </p>
                )}
              </div>
            ) : (
              <div className="rounded-lg border border-dashed px-3 py-4 text-sm text-muted-foreground">
                No recent execution for this issue.
              </div>
            )}
          </div>

          <Separator />

          <div className="space-y-2">
            <div className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
              <MessageSquare className="h-3.5 w-3.5" />
              Recent activity
            </div>
            <div className="space-y-2">
              {recentEntries.length === 0 ? (
                <div className="rounded-lg border border-dashed px-3 py-4 text-sm text-muted-foreground">
                  No activity yet.
                </div>
              ) : (
                recentEntries.map((entry) => {
                  const item = describeActivity(entry, getActorName);
                  return (
                    <div
                      key={entry.id}
                      className="rounded-lg border px-3 py-2.5"
                    >
                      <div className="flex items-start gap-2">
                        <div className="mt-0.5 shrink-0">
                          {entry.type === "comment" ? (
                            <MessageSquare className="h-4 w-4 text-muted-foreground" />
                          ) : item.body.includes("priority") ? (
                            <PriorityIcon
                              priority={
                                ((entry.details ?? {}) as Record<string, string>)
                                  .to as IssuePriority
                              }
                              className="h-4 w-4"
                            />
                          ) : item.body.includes("status") ? (
                            <StatusIcon
                              status={
                                ((entry.details ?? {}) as Record<string, string>)
                                  .to as IssueStatus
                              }
                              className="h-4 w-4"
                            />
                          ) : (
                            <MessageSquare className="h-4 w-4 text-muted-foreground" />
                          )}
                        </div>
                        <div className="min-w-0 flex-1">
                          <div className="flex items-center gap-2">
                            <span className="truncate text-sm font-medium">
                              {item.title}
                            </span>
                            <span className="shrink-0 text-xs text-muted-foreground">
                              {timeAgo(entry.created_at)}
                            </span>
                          </div>
                          <p
                            className={cn(
                              "mt-1 text-sm text-muted-foreground",
                              item.kind === "comment" && "line-clamp-3",
                            )}
                          >
                            {item.body}
                          </p>
                        </div>
                      </div>
                    </div>
                  );
                })
              )}
            </div>
          </div>

          <Separator />

          <div className="rounded-lg border px-3 py-3 text-xs text-muted-foreground">
            Created {timeAgo(issue.created_at)}. Due {shortDate(issue.due_date)}.
            <Button
              variant="link"
              size="sm"
              className="h-auto px-1 text-xs"
              onClick={onExpand}
            >
              Open full detail
            </Button>
          </div>
        </div>
      </ScrollArea>
    </div>
  );
}
