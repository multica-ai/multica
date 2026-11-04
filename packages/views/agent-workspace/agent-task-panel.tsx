"use client";

import { useId, useState } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import {
  agentTasksOptions,
  agentWorkspaceIssuesOptions,
} from "@multica/core/agents/queries";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { useModalStore } from "@multica/core/modals";
import { useWorkspacePaths } from "@multica/core/paths";
import type { Agent, AgentTask, ChatSession, Issue } from "@multica/core/types";
import {
  Button,
  Card,
  Spinner,
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@nextop-os/ui-system/components";
import { AddIcon, LinkIcon, TaskIcon } from "@nextop-os/ui-system/icons";
import { AppLink } from "../navigation";
import { useT, useTimeAgo } from "../i18n";
import { formatDuration } from "../dashboard/utils";
import {
  cancellationActorLabel,
  runOutcomeLabel,
} from "../agents/components/tabs/task-failure";
import { StatusIcon } from "../issues/components/status-icon";
import { TaskStatusIcon } from "../issues/components/task-status-icon";
import { useStatusLabel } from "../issues/utils/status-label";

export function AgentTaskPanel({
  wsId,
  agent,
  sessions = [],
  canInvoke,
  canReference,
  referenceDisabledReason,
  onReference,
  onRevealConversation,
}: {
  wsId: string;
  agent: Agent | null;
  sessions?: ChatSession[];
  canInvoke: boolean;
  canReference: boolean;
  referenceDisabledReason?: string;
  onReference: (issue: Issue) => void;
  onRevealConversation: () => void;
}) {
  const { t } = useT("chat");
  const paths = useWorkspacePaths();
  const [tab, setTab] = useState<"issues" | "runs">("issues");
  const issues = useInfiniteQuery(
    agentWorkspaceIssuesOptions(wsId, agent?.id ?? ""),
  );
  const runs = useInfiniteQuery({
    ...agentTasksOptions(wsId, agent?.id ?? ""),
    enabled: Boolean(agent && tab === "runs"),
  });
  const statuses = useIssueStatuses(wsId);
  const statusLabel = useStatusLabel(wsId);
  const activeQuery = tab === "issues" ? issues : runs;
  const issueRows = uniqueRows(
    issues.data?.pages.flatMap((page) => page.issues) ?? [],
  );
  const runRows = uniqueRows(runs.data?.pages.flatMap((page) => page.tasks) ?? []);
  const issueById = new Map(issueRows.map((issue) => [issue.id, issue]));
  const sessionById = new Map(sessions.map((session) => [session.id, session]));

  return (
    <TooltipProvider>
      <aside
        className="agent-workspace-chrome flex min-h-0 flex-col bg-background"
        aria-label={t(($) => $.workbench.tasks)}
      >
        <div className="flex h-12 shrink-0 items-center justify-between gap-2 border-b px-3">
          <h2 className="flex items-center gap-2 text-label font-medium">
            <TaskIcon className="size-3.5 text-muted-foreground" />
            {t(($) => $.workbench.tasks)}
          </h2>
          <Button
            size="icon-xs"
            variant="ghost"
            disabled={!agent || !canInvoke}
            aria-label={t(($) => $.workbench.create_task)}
            onClick={() => {
              if (agent)
                useModalStore.getState().open("create-issue", {
                  assignee_type: "agent",
                  assignee_id: agent.id,
                });
            }}
          >
            <AddIcon className="size-3.5" />
          </Button>
        </div>
        <div
          className="flex gap-1 border-b px-3 py-2"
          role="group"
          aria-label={t(($) => $.workbench.task_views)}
        >
          <Button
            size="xs"
            variant={tab === "issues" ? "secondary" : "ghost"}
            aria-pressed={tab === "issues"}
            onClick={() => setTab("issues")}
          >
            {t(($) => $.workbench.assigned_tasks)}
          </Button>
          <Button
            size="xs"
            variant={tab === "runs" ? "secondary" : "ghost"}
            aria-pressed={tab === "runs"}
            onClick={() => setTab("runs")}
          >
            {t(($) => $.workbench.runs)}
          </Button>
        </div>
        <div
          className="min-h-0 flex-1 overflow-y-auto p-2"
          aria-busy={activeQuery.isFetching}
        >
          {!agent ? (
            <p className="px-2 py-4 text-caption text-muted-foreground">
              {t(($) => $.workbench.select_agent)}
            </p>
          ) : activeQuery.isPending ? (
            <div role="status" className="flex items-center gap-2">
              <Spinner size={16} />
              {t(($) => $.workbench.loading)}
            </div>
          ) : activeQuery.isError && !activeQuery.data ? (
            <div role="alert" className="space-y-2">
              <p>{t(($) => $.workbench.load_error)}</p>
              <Button
                variant="outline"
                size="sm"
                disabled={activeQuery.isFetching}
                aria-busy={activeQuery.isFetching}
                onClick={() => void activeQuery.refetch()}
              >
                {t(($) => $.workbench.retry)}
              </Button>
            </div>
          ) : (
            <>
              {tab === "issues" ? (
                <div className="divide-y divide-border">
                  {issueRows.map((issue) => (
                    <Card
                      size="sm"
                      key={issue.id}
                      className="agent-workspace-task-row"
                    >
                      <AppLink
                        href={paths.issueDetail(issue.identifier || issue.id)}
                        className="flex min-w-0 items-start gap-2 rounded-sm outline-none hover:underline focus-visible:ring-2 focus-visible:ring-ring"
                      >
                        <StatusIcon
                          status={issue.status}
                          category={statuses.categoryOf(issue.status)}
                          color={statuses.entryOf(issue.status)?.color}
                          icon={statuses.entryOf(issue.status)?.icon}
                          className="mt-0.5 size-3.5 shrink-0"
                        />
                        <span className="min-w-0 flex-1">
                          <span className="block font-mono text-caption text-muted-foreground">
                            {issue.identifier}
                          </span>
                          <span className="block break-words text-body font-medium">
                            {issue.title}
                          </span>
                        </span>
                      </AppLink>
                      <div className="flex items-center justify-between gap-2 pl-5.5">
                        <span className="text-caption text-muted-foreground">
                          {statusLabel(issue.status)}
                        </span>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <span
                              className="inline-flex rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
                              tabIndex={!canReference ? 0 : undefined}
                              aria-label={
                                !canReference ? referenceDisabledReason : undefined
                              }
                            >
                              <Button
                                variant="ghost"
                                size="icon-xs"
                                disabled={!canReference}
                                aria-label={t(($) => $.workbench.reference_task, {
                                  identifier: issue.identifier,
                                })}
                                onClick={() => onReference(issue)}
                              >
                                <LinkIcon className="size-3.5 text-muted-foreground" />
                              </Button>
                            </span>
                          </TooltipTrigger>
                          <TooltipContent>
                            {!canReference && referenceDisabledReason
                              ? referenceDisabledReason
                              : t(($) => $.workbench.reference_task, {
                                  identifier: issue.identifier,
                                })}
                          </TooltipContent>
                        </Tooltip>
                      </div>
                    </Card>
                  ))}
                  {issueRows.length === 0 && (
                    <p className="px-2 py-4 text-caption text-muted-foreground">
                      {t(($) => $.workbench.no_tasks)}
                    </p>
                  )}
                </div>
              ) : (
                <div className="divide-y divide-border">
                  {runRows.map((run) => (
                    <AgentRunRow
                      key={run.id}
                      run={run}
                      issue={issueById.get(run.issue_id)}
                      session={sessionById.get(run.chat_session_id ?? "")}
                      onRevealConversation={onRevealConversation}
                    />
                  ))}
                  {runRows.length === 0 && (
                    <p className="px-2 py-4 text-caption text-muted-foreground">
                      {t(($) => $.workbench.no_runs)}
                    </p>
                  )}
                </div>
              )}
              {activeQuery.hasNextPage && !activeQuery.isError && (
                <Button
                  className="mt-3 w-full"
                  variant="outline"
                  disabled={activeQuery.isFetching}
                  aria-busy={activeQuery.isFetchingNextPage}
                  onClick={() => void activeQuery.fetchNextPage()}
                >
                  {activeQuery.isFetchingNextPage && <Spinner size={14} />}
                  {t(($) => $.workbench.load_more)}
                </Button>
              )}
              {activeQuery.isError && (
                <div role="alert" className="mt-3 space-y-2 px-2 text-caption">
                  <p className="text-destructive">
                    {t(($) => $.workbench.load_error)}
                  </p>
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={activeQuery.isFetching}
                    aria-busy={activeQuery.isFetching}
                    onClick={() =>
                      void (activeQuery.isFetchNextPageError
                        ? activeQuery.fetchNextPage()
                        : activeQuery.refetch())
                    }
                  >
                    {activeQuery.isFetching && <Spinner size={14} />}
                    {t(($) => $.workbench.retry)}
                  </Button>
                </div>
              )}
            </>
          )}
        </div>
      </aside>
    </TooltipProvider>
  );
}

function uniqueRows<T extends { id: string }>(rows: T[]): T[] {
  return [...new Map(rows.map((row) => [row.id, row])).values()];
}

function AgentRunRow({
  run,
  issue,
  session,
  onRevealConversation,
}: {
  run: AgentTask;
  issue?: Issue;
  session?: ChatSession;
  onRevealConversation: () => void;
}) {
  const { t } = useT("chat");
  const { t: agentsT } = useT("agents");
  const paths = useWorkspacePaths();
  const timeAgo = useTimeAgo();
  const titleId = useId();
  const terminal = ["completed", "failed", "cancelled"].includes(run.status);
  const source =
    run.kind === "quick_create"
      ? terminal
        ? agentsT(($) => $.tab_body.activity.source_quick_create)
        : agentsT(($) => $.tab_body.activity.source_creating_issue)
      : run.issue_id
        ? agentsT(($) => $.tab_body.activity.source_issue)
        : run.chat_session_id
          ? agentsT(($) => $.tab_body.activity.source_chat)
          : run.autopilot_run_id
            ? agentsT(($) => $.tab_body.activity.source_autopilot)
            : agentsT(($) => $.tab_body.activity.source_untracked);
  const title = issue?.title || session?.title || run.trigger_summary || source;
  const status =
    cancellationActorLabel(run, agentsT)
    ?? t(($) => $.workbench.run_status[run.status]);
  const outcome = runOutcomeLabel(run, agentsT);
  const timestamp = terminal
    ? run.completed_at ?? run.created_at
    : run.started_at ?? run.dispatched_at ?? run.created_at;
  const validTime = Number.isFinite(Date.parse(timestamp));
  const relativeTime = validTime ? timeAgo(timestamp) : null;
  const time = !relativeTime
    ? null
    : terminal
      ? relativeTime
      : run.status === "running" && run.started_at
        ? agentsT(($) => $.tab_body.activity.started_prefix, { when: relativeTime })
        : run.status === "dispatched" && run.dispatched_at
          ? agentsT(($) => $.tab_body.activity.dispatched_prefix, { when: relativeTime })
          : agentsT(($) => $.tab_body.activity.queued_prefix, { when: relativeTime });
  const durationMs =
    terminal && run.started_at && run.completed_at
      ? Date.parse(run.completed_at) - Date.parse(run.started_at)
      : 0;

  return (
    <Card size="sm" className="agent-workspace-task-row" title={run.id}>
      <div className="flex min-w-0 items-center gap-2 text-caption text-muted-foreground">
        <span className="truncate">{source}</span>
        {issue?.identifier && (
          <span className="truncate font-mono">{issue.identifier}</span>
        )}
      </div>
      <p id={titleId} className="line-clamp-2 break-words text-body font-medium">
        {title}
      </p>
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-caption text-muted-foreground">
        <span className="flex items-center gap-1.5">
          <TaskStatusIcon status={run.status} />
          {status}
        </span>
        {time && <time dateTime={timestamp}>{time}</time>}
        {durationMs > 0 && (
          <span>{formatDuration(durationMs / 1000, "<1s")}</span>
        )}
      </div>
      {outcome && outcome !== status && (
        <p
          className={`break-words text-caption ${run.status === "failed" ? "text-destructive" : "text-muted-foreground"}`}
        >
          {outcome}
        </p>
      )}
      <div className="flex flex-wrap gap-x-3 gap-y-1 text-caption">
        {run.issue_id && (
          <AppLink
            href={paths.issueDetail(run.issue_id)}
            aria-describedby={titleId}
            className="rounded-sm outline-none hover:underline focus-visible:ring-2 focus-visible:ring-ring"
          >
            {t(($) => $.workbench.open_task)}
          </AppLink>
        )}
        {run.chat_session_id && (
          <AppLink
            href={paths.chatSession(run.chat_session_id)}
            aria-describedby={titleId}
            onClick={onRevealConversation}
            className="rounded-sm outline-none hover:underline focus-visible:ring-2 focus-visible:ring-ring"
          >
            {t(($) => $.workbench.open_conversation)}
          </AppLink>
        )}
      </div>
    </Card>
  );
}
