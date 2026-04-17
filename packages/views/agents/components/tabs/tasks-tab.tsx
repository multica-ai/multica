"use client";

import { useState, useEffect, useMemo } from "react";
import { ListTodo, RotateCcw, Copy } from "lucide-react";
import type { Agent, AgentExternalSession, AgentTask } from "@multica/core/types";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { issueListOptions } from "@multica/core/issues/queries";
import { useQuery } from "@tanstack/react-query";
import { AppLink } from "../../../navigation";
import { toast } from "sonner";
import { taskStatusConfig } from "../../config";

type ResumeEntry = {
  session_id: string;
  work_dir?: string;
  issue_id?: string;
  source_task_id?: string;
  source: "task" | "external";
  timestamp: string;
};

function shortSessionId(sessionId: string): string {
  if (sessionId.length <= 20) return sessionId;
  return `${sessionId.slice(0, 8)}...${sessionId.slice(-8)}`;
}

function resolveTaskResumeCommand(task: AgentTask): string | null {
  const explicitCommand = (task.resume_command || "").trim();
  if (explicitCommand) return explicitCommand;
  const resumeSessionID = (
    task.resume_session_id ||
    task.prior_session_id ||
    task.session_id ||
    ""
  ).trim();
  return resumeSessionID ? `codex resume ${resumeSessionID}` : null;
}

export function TasksTab({ agent }: { agent: Agent }) {
  const [tasks, setTasks] = useState<AgentTask[]>([]);
  const [externalSessions, setExternalSessions] = useState<AgentExternalSession[]>([]);
  const [loading, setLoading] = useState(true);
  const [resumingSessionId, setResumingSessionId] = useState<string | null>(null);
  const [issueBindingBySession, setIssueBindingBySession] = useState<Record<string, string>>({});
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { data: issues = [] } = useQuery(issueListOptions(wsId));

  const loadData = async () => {
    setLoading(true);
    try {
      const [taskList, externalList] = await Promise.all([
        api.listAgentTasks(agent.id),
        api.listAgentExternalSessions(agent.id, { days: 7 }),
      ]);
      setTasks(taskList);
      setExternalSessions(externalList);
    } catch {
      setTasks([]);
      setExternalSessions([]);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void loadData();
  }, [agent.id]);

  const activeStatuses = ["running", "dispatched", "queued"];
  const sortedTasks = useMemo(
    () =>
      [...tasks].sort((a, b) => {
        const aActive = activeStatuses.indexOf(a.status);
        const bActive = activeStatuses.indexOf(b.status);
        const aIsActive = aActive !== -1;
        const bIsActive = bActive !== -1;
        if (aIsActive && !bIsActive) return -1;
        if (!aIsActive && bIsActive) return 1;
        if (aIsActive && bIsActive) return aActive - bActive;
        return new Date(b.created_at).getTime() - new Date(a.created_at).getTime();
      }),
    [tasks],
  );

  const issueMap = new Map(issues.map((i) => [i.id, i]));
  const bindableIssues = useMemo(
    () =>
      issues
        .filter(
          (issue) =>
            issue.assignee_type === "agent" &&
            issue.assignee_id === agent.id &&
            issue.status !== "done" &&
            issue.status !== "cancelled",
        )
        .sort(
          (a, b) =>
            Date.parse(b.updated_at || b.created_at) - Date.parse(a.updated_at || a.created_at),
        ),
    [issues, agent.id],
  );

  const resumeEntries = useMemo(() => {
    const sevenDaysAgo = Date.now() - 7 * 24 * 60 * 60 * 1000;
    const bySession = new Map<string, ResumeEntry>();

    for (const external of externalSessions) {
      if (!external.session_id) continue;
      const seenAt = Date.parse(external.last_seen_at);
      if (!Number.isNaN(seenAt) && seenAt < sevenDaysAgo) continue;
      bySession.set(external.session_id, {
        session_id: external.session_id,
        work_dir: external.work_dir,
        issue_id: external.issue_id,
        source_task_id: external.source_task_id,
        source: "external",
        timestamp: external.last_seen_at,
      });
    }

    const taskCandidates = tasks
      .filter((task) => task.status === "completed" && !!task.session_id)
      .filter((task) => {
        const ts = Date.parse(task.completed_at ?? task.created_at);
        return !Number.isNaN(ts) && ts >= sevenDaysAgo;
      })
      .sort(
        (a, b) =>
          Date.parse(b.completed_at ?? b.created_at) -
          Date.parse(a.completed_at ?? a.created_at),
      );

    for (const task of taskCandidates) {
      const sessionId = task.session_id!;
      bySession.set(sessionId, {
        session_id: sessionId,
        work_dir: task.work_dir,
        issue_id: task.issue_id || undefined,
        source_task_id: task.id,
        source: "task",
        timestamp: task.completed_at ?? task.created_at,
      });
    }

    return [...bySession.values()].sort(
      (a, b) => Date.parse(b.timestamp) - Date.parse(a.timestamp),
    );
  }, [tasks, externalSessions]);

  const copySessionId = async (sessionId: string) => {
    try {
      await navigator.clipboard.writeText(sessionId);
      toast.success("Session ID copied");
    } catch {
      toast.error("Failed to copy Session ID");
    }
  };

  const handleResume = async (entry: ResumeEntry) => {
    setResumingSessionId(entry.session_id);
    try {
      if (entry.source_task_id) {
        await api.resumeAgentTask(agent.id, entry.source_task_id);
      } else {
        const selectedIssueId = issueBindingBySession[entry.session_id];
        const autoIssueId =
          !entry.issue_id && !selectedIssueId && bindableIssues.length === 1
            ? bindableIssues[0]!.id
            : undefined;
        const effectiveIssueID = entry.issue_id || selectedIssueId || autoIssueId;

        await api.resumeAgentExternalSession(agent.id, {
          session_id: entry.session_id,
          work_dir: entry.work_dir,
          issue_id: effectiveIssueID,
        });
        if (!effectiveIssueID) {
          toast.info("This run is not bound to an issue, it will show in Tasks only.");
        }
      }
      toast.success("Resume task queued");
      await loadData();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to queue resume task");
    } finally {
      setResumingSessionId(null);
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
      <div>
        <h3 className="text-sm font-semibold">Task Queue</h3>
        <p className="text-xs text-muted-foreground mt-0.5">
          Issues assigned to this agent and their execution status.
        </p>
      </div>

      <div className="rounded-lg border p-3">
        <div className="mb-2 flex items-center justify-between">
          <div>
            <h4 className="text-sm font-semibold">Resume Sessions (7d)</h4>
            <p className="text-xs text-muted-foreground">
              Operate concrete resumable sessions, similar to codex resume list.
            </p>
          </div>
          <span className="text-xs text-muted-foreground">
            {resumeEntries.length} sessions
          </span>
        </div>

        {resumeEntries.length === 0 ? (
          <div className="space-y-1">
            <p className="text-xs text-muted-foreground">
              No resumable sessions from the last 7 days.
            </p>
            <p className="text-[11px] text-muted-foreground">
              If sessions exist on host, mount host <code>~/.codex</code> into backend and set{" "}
              <code>MULTICA_CODEX_SESSIONS_ROOT</code>.
            </p>
          </div>
        ) : (
          <div className="space-y-1.5">
            {resumeEntries.map((entry) => {
              const issue = entry.issue_id ? issueMap.get(entry.issue_id) : undefined;
              const isResuming = resumingSessionId === entry.session_id;
              return (
                <div
                  key={`resume-${entry.session_id}`}
                  className="flex items-center gap-3 rounded-md border px-3 py-2"
                >
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">
                        {shortSessionId(entry.session_id)}
                      </span>
                      <span className="rounded border px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
                        {entry.source}
                      </span>
                      {issue ? (
                        <span className="truncate text-xs text-muted-foreground">
                          {issue.identifier} - {issue.title}
                        </span>
                      ) : (
                        <span className="truncate text-xs text-muted-foreground">
                          No issue bound
                        </span>
                      )}
                    </div>
                    <div className="mt-0.5 text-[11px] text-muted-foreground">
                      {entry.source === "task" ? "Completed" : "Last seen"}{" "}
                      {new Date(entry.timestamp).toLocaleString()}
                    </div>
                    {!entry.issue_id && !entry.source_task_id && (
                      <div className="mt-1.5 flex items-center gap-2">
                        <span className="text-[11px] text-muted-foreground">Bind issue:</span>
                        <select
                          className="h-7 min-w-[180px] rounded border bg-background px-2 text-xs"
                          value={issueBindingBySession[entry.session_id] ?? ""}
                          onChange={(e) =>
                            setIssueBindingBySession((prev) => ({
                              ...prev,
                              [entry.session_id]: e.target.value,
                            }))
                          }
                        >
                          <option value="">Tasks only (no issue)</option>
                          {bindableIssues.map((bindIssue) => (
                            <option key={bindIssue.id} value={bindIssue.id}>
                              {bindIssue.identifier} - {bindIssue.title}
                            </option>
                          ))}
                        </select>
                      </div>
                    )}
                  </div>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => copySessionId(entry.session_id)}
                  >
                    <Copy className="h-3.5 w-3.5" />
                    Copy ID
                  </Button>
                  <Button size="sm" disabled={isResuming} onClick={() => handleResume(entry)}>
                    <RotateCcw className="h-3.5 w-3.5" />
                    {isResuming ? "Queueing..." : "Continue"}
                  </Button>
                </div>
              );
            })}
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
            const config = taskStatusConfig[task.status] ?? taskStatusConfig.queued!;
            const Icon = config.icon;
            const issue = issueMap.get(task.issue_id);
            const isActive = task.status === "running" || task.status === "dispatched";
            const isRunning = task.status === "running";
            const rowClassName = `flex items-center gap-3 rounded-lg border px-4 py-3 transition-shadow hover:shadow-sm ${
              isRunning
                ? "border-success/40 bg-success/5"
                : task.status === "dispatched"
                  ? "border-info/40 bg-info/5"
                  : ""
            }`;
            const resumeCommand = resolveTaskResumeCommand(task);
            const issueTitle = issue
              ? issue.title
              : task.issue_id
                ? `Issue ${task.issue_id.slice(0, 8)}...`
                : resumeCommand ?? "Manual resume session";

            const content = (
              <>
                <Icon
                  className={`h-4 w-4 shrink-0 ${config.color} ${
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
                    <span className={`text-sm truncate ${isActive ? "font-medium" : ""}`}>
                      {issueTitle}
                    </span>
                  </div>
                  <div className="mt-0.5 text-xs text-muted-foreground">
                    {isRunning && task.started_at
                      ? `Started ${new Date(task.started_at).toLocaleString()}`
                      : task.status === "dispatched" && task.dispatched_at
                        ? `Dispatched ${new Date(task.dispatched_at).toLocaleString()}`
                        : task.status === "completed" && task.completed_at
                          ? `Completed ${new Date(task.completed_at).toLocaleString()}`
                          : task.status === "failed" && task.completed_at
                            ? `Failed ${new Date(task.completed_at).toLocaleString()}`
                            : `Queued ${new Date(task.created_at).toLocaleString()}`}
                  </div>
                  {resumeCommand && (
                    <div className="mt-1">
                      <code
                        className="inline-block max-w-full truncate rounded bg-muted px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground"
                        title={resumeCommand}
                      >
                        {resumeCommand}
                      </code>
                    </div>
                  )}
                </div>
                <span className={`shrink-0 text-xs font-medium ${config.color}`}>
                  {config.label}
                </span>
              </>
            );

            return (
              task.issue_id ? (
                <AppLink
                  key={task.id}
                  href={paths.issueDetail(task.issue_id)}
                  className={`${rowClassName} text-foreground no-underline hover:no-underline`}
                >
                  {content}
                </AppLink>
              ) : (
                <div key={task.id} className={rowClassName}>
                  {content}
                </div>
              )
            );
          })}
        </div>
      )}
    </div>
  );
}
