"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Bell, Clock3, ChevronRight, GitPullRequest, Link2, ListChecks, CircleDot, Play } from "lucide-react";
import { toast } from "sonner";
import {
  issueSystemWakeupsOptions,
  issueWakeupRunsOptions,
  issueWakeupsOptions,
  useDeleteIssueWakeup,
  useDisableIssueWakeup,
  useEnableIssueWakeup,
  useTriggerIssueWakeup,
  issueTasksOptions,
} from "@multica/core/issues";
import type { AgentTask, IssueWakeup, WakeupCondition } from "@multica/core/types";
import { useCurrentWorkspace } from "@multica/core/paths";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
  PopoverTitle,
} from "@multica/ui/components/ui/popover";
import { Button } from "@multica/ui/components/ui/button";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { WakeupInstructionEditor } from "./wakeup-instruction-editor";
import { WakeupControl } from "./wakeup-control";
import { WakeupCreate } from "./wakeup-create";
import { SystemWakeupRow } from "./system-wakeup-row";
import { TranscriptButton } from "../../common/task-transcript";
import { useViewingTimezone } from "../../common/use-viewing-timezone";
import { useLocale, useT } from "../../i18n";
import {
  formatWakeupTime,
  isCurrentWakeup,
  isActiveWakeupRun,
  useWakeupText,
  wakeupRun,
} from "./wakeup-presentation";

/** The glyph a rule's condition reads with in lists and the timeline. */
export function conditionIcon(condition?: WakeupCondition | null) {
  switch (condition?.type) {
    case "children_done":
      return ListChecks;
    case "pull_request":
      return GitPullRequest;
    case "other_issue":
      return Link2;
    case "issue_field":
      return CircleDot;
    default:
      return null;
  }
}

/** The rule's latest runs: what fired, and what came of it. */
function WakeupHistory({ wakeup }: { wakeup: IssueWakeup }) {
  const { t } = useT("issues");
  const locale = useLocale();
  const viewTZ = useViewingTimezone();
  const workspaceId = useCurrentWorkspace()?.id ?? "";
  const text = useWakeupText();
  const { data: runs, isError } = useQuery(issueWakeupRunsOptions(workspaceId, wakeup.issue_id, wakeup.id));
  return (
    <div className="space-y-1.5">
      <p className="text-caption font-medium">{t(($) => $.wakeups.detail.history_title)}</p>
      {isError ? (
        <p className="text-caption text-muted-foreground">{t(($) => $.wakeups.detail.history_error)}</p>
      ) : runs && runs.length === 0 ? (
        <p className="text-caption text-muted-foreground">{t(($) => $.wakeups.detail.history_empty)}</p>
      ) : (
        <ul className="space-y-1">
          {(runs ?? []).map((run) => (
            <li key={run.id} className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 text-caption">
              <span className="tabular-nums text-muted-foreground">{formatWakeupTime(run.created_at, locale, viewTZ)}</span>
              <span className="min-w-0 break-words">
                {run.checkin_note ? (
                  <>
                    <span>{t(($) => $.wakeups.detail.run_checkin)}</span>
                    <span className="text-muted-foreground"> · {run.checkin_note}</span>
                  </>
                ) : run.commented ? (
                  t(($) => $.wakeups.detail.run_commented, { state: text.runState(run.status) })
                ) : (
                  text.runState(run.status)
                )}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function DeleteWakeup({ wakeup, onDeleted }: { wakeup: IssueWakeup; onDeleted: () => void }) {
  const { t } = useT("issues");
  const text = useWakeupText();
  const workspaceId = useCurrentWorkspace()?.id ?? "";
  const remove = useDeleteIssueWakeup(workspaceId, wakeup.issue_id);
  const [open, setOpen] = useState(false);
  return (
    <AlertDialog open={open} onOpenChange={(next) => !remove.isPending && setOpen(next)}>
      <Button variant="ghost" size="sm" className="ml-auto text-destructive hover:text-destructive" onClick={() => setOpen(true)}>
        {t(($) => $.wakeups.detail.delete)}
      </Button>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.wakeups.detail.delete_title)}</AlertDialogTitle>
          <AlertDialogDescription>{t(($) => $.wakeups.detail.delete_body, { agent: wakeup.agent_name })}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={remove.isPending}>{t(($) => $.wakeups.create.cancel)}</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={remove.isPending}
            onClick={(event) => {
              event.preventDefault();
              remove.mutate(wakeup.id, {
                onSuccess: () => {
                  setOpen(false);
                  onDeleted();
                },
                onError: (err) => toast.error(text.error(err, t(($) => $.wakeups.detail.delete_error))),
              });
            }}
          >
            {remove.isPending ? t(($) => $.wakeups.detail.deleting) : t(($) => $.wakeups.detail.delete)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

function WakeupRow({
  wakeup,
  task,
  sourceTask,
  pending,
  onDisable,
  onEnable,
  closed,
}: {
  wakeup: IssueWakeup;
  task?: AgentTask;
  sourceTask?: AgentTask;
  pending: boolean;
  onDisable: () => void;
  closed: boolean;
  onEnable: (input?: { at?: string; rearm?: boolean }) => Promise<void>;
}) {
  const { t } = useT("issues");
  const workspaceId = useCurrentWorkspace()?.id ?? "";
  const text = useWakeupText();
  const viewTZ = useViewingTimezone();
  const status = task?.status ?? wakeup.last_task_status;
  const activeRun = isActiveWakeupRun(status);
  const Icon = conditionIcon(wakeup.condition) ?? (wakeup.kind === "event" ? Bell : Clock3);
  const [detailOpen, setDetailOpen] = useState(false);
  const trigger = useTriggerIssueWakeup(workspaceId, wakeup.issue_id);
  const paused = text.paused(wakeup);
  const fires = text.fires(wakeup);
  return (
    <div
      className="grid grid-cols-[minmax(0,1fr)_auto]"
      aria-busy={pending}
    >
      <Popover open={detailOpen} onOpenChange={setDetailOpen}>
        <PopoverTrigger
          render={
            <button
              type="button"
              className="col-span-2 col-start-1 row-start-1 grid min-w-0 grid-cols-subgrid rounded-md py-1.5 text-left text-caption hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring"
            />
          }
        >
          <span className="flex min-h-8 min-w-0 items-start gap-2 py-1 pl-2 pr-1">
            <Icon
              className="mt-px size-3.5 shrink-0 text-muted-foreground"
              aria-hidden="true"
            />
            <span className="line-clamp-2 min-w-0 break-words font-medium">
              {text.trigger(wakeup)}
            </span>
          </span>
          <span className="col-span-2 min-w-0 break-words pl-7.5 pr-2">
            <span className="block text-muted-foreground">
              {text.summary(wakeup, closed)}
            </span>
            {status && (
              <span className="block text-muted-foreground">
                {t(
                  ($) =>
                    activeRun || wakeup.mode === "once"
                      ? $.wakeups.execution_summary
                      : $.wakeups.recent_execution,
                  { state: text.runState(status) },
                )}
              </span>
            )}
            {(wakeup.disabled_at || closed) && activeRun && (
              <span className="block text-muted-foreground">
                {t(($) => $.wakeups.stopped_running)}
              </span>
            )}
            {paused && <span className="block text-warning">{paused}</span>}
            {wakeup.last_error && (
              <span className="block text-destructive">
                {t(($) => $.wakeups.needs_attention)}
              </span>
            )}
          </span>
        </PopoverTrigger>
        <PopoverContent
          align="end"
          className="max-h-[70dvh] w-80 max-w-[calc(100vw-2rem)] overflow-y-auto"
          keepMounted
        >
          <PopoverTitle>{text.trigger(wakeup)}</PopoverTitle>
          <p className="text-caption text-muted-foreground">
            {t(($) => $.wakeups.wake_agent, { agent: wakeup.agent_name })} ·{" "}
            {text.schedule(wakeup)}
          </p>
          <p className="text-caption text-muted-foreground">
            {t(($) => $.wakeups.scope_title)}:{" "}
            {t(($) => $.wakeups.scope_current)}
          </p>
          {wakeup.created_by_name && (
            <p className="break-words text-caption text-muted-foreground">
              {t(($) => $.wakeups.source_title)}: {text.source(wakeup)}
            </p>
          )}
          {wakeup.expires_at && (
            <p className="break-words text-caption text-muted-foreground">
              {t(($) => $.wakeups.expiry_title)}: {text.expiry(wakeup)}
            </p>
          )}
          {fires && <p className="text-caption text-muted-foreground">{fires}</p>}
          {paused && (
            <p className="break-words text-caption text-warning">
              {paused} · {t(($) => $.wakeups.paused.hint)}
            </p>
          )}
          <div className="flex items-center justify-between gap-2">
            <p className="text-caption font-medium">{t(($) => $.wakeups.instruction_title)}</p>
            <WakeupInstructionEditor workspaceId={workspaceId} issueId={wakeup.issue_id} wakeupId={wakeup.id} />
          </div>
          <p className="whitespace-pre-wrap break-words text-caption">
            {wakeup.instruction}
          </p>
          {wakeup.kind === "event" && !wakeup.condition && (
            <p className="break-words text-caption text-muted-foreground">
              {t(($) => $.wakeups.any_event)}:{" "}
              {wakeup.event_types
                .map((event) =>
                  text.eventCondition(event, wakeup),
                )
                .join("; ")}
            </p>
          )}
          {wakeup.filter_actor_type && (
            <p className="break-words text-caption text-muted-foreground">
              {t(($) => $.wakeups.source_actor)}: {text.actorName(wakeup)}
            </p>
          )}
          {wakeup.filter_agent_id && (
            <p className="break-all text-caption text-muted-foreground">
              {t(($) => $.wakeups.source_agent)}:{" "}
              {wakeup.filter_agent_name ?? wakeup.filter_agent_id}
            </p>
          )}
          {sourceTask && (
            <TranscriptButton
              task={sourceTask}
              agentName={wakeup.filter_agent_name ?? ""}
              title={t(($) => $.wakeups.source_run)}
            />
          )}
          {wakeup.filter_task_id && (
            <p className="break-all text-caption text-muted-foreground">
              {t(($) => $.wakeups.source_run)}: {wakeup.filter_task_id}
            </p>
          )}
          {wakeup.next_fire_at && (
            <p className="text-caption text-muted-foreground">
              {new Date(wakeup.next_fire_at).toLocaleString(undefined, {
                timeZone: viewTZ,
              })}{" "}
              · {viewTZ}
            </p>
          )}
          {wakeup.cron_expression && (
            <code className="block text-caption text-muted-foreground">
              {wakeup.cron_expression} · {wakeup.timezone}
            </code>
          )}
          {wakeup.last_error && (
            <p className="break-words text-caption text-destructive">
              {wakeup.last_error}
            </p>
          )}
          {task && (
            <div className="flex items-center gap-1 text-caption text-muted-foreground">
              <span>{t(($) => $.wakeups.last_run)}</span>
              <TranscriptButton
                task={task}
                agentName={wakeup.agent_name}
                title={t(($) => $.wakeups.last_run)}
              />
            </div>
          )}
          {detailOpen && <WakeupHistory wakeup={wakeup} />}
          {!closed && (
            <div className="flex items-center gap-1 border-t border-border pt-2.5">
              {!wakeup.disabled_at && (
                <Button
                  variant="outline"
                  size="sm"
                  disabled={trigger.isPending}
                  onClick={() =>
                    trigger.mutate(wakeup.id, {
                      onSuccess: () => toast.success(t(($) => $.wakeups.detail.wake_now_done, { agent: wakeup.agent_name })),
                      onError: (err) => toast.error(text.error(err, t(($) => $.wakeups.detail.wake_now_error))),
                    })
                  }
                >
                  <Play aria-hidden="true" />
                  {t(($) => $.wakeups.detail.wake_now)}
                </Button>
              )}
              <DeleteWakeup wakeup={wakeup} onDeleted={() => setDetailOpen(false)} />
            </div>
          )}
        </PopoverContent>
      </Popover>
      <div className="z-10 col-start-2 row-start-1 self-start">
        <WakeupControl
          wakeup={wakeup}
          task={task}
          pending={pending}
          closed={closed}
          onDisable={onDisable}
          onEnable={onEnable}
        />
      </div>
    </div>
  );
}

export function WakeupsSection({
  issueId,
  closed = false,
  defaultAgentId,
}: {
  issueId: string;
  closed?: boolean;
  /** Preselected target for a new wakeup: the issue's agent assignee. */
  defaultAgentId?: string;
}) {
  const { t } = useT("issues");
  const workspaceId = useCurrentWorkspace()?.id ?? "";
  const [open, setOpen] = useState(true);
  const [historyOpen, setHistoryOpen] = useState(false);
  const {
    data = [],
    isError,
    refetch,
  } = useQuery(issueWakeupsOptions(workspaceId, issueId));
  const { data: tasks = [] } = useQuery(issueTasksOptions(issueId));
  const { data: systemRules = [] } = useQuery(issueSystemWakeupsOptions(workspaceId, issueId));
  const text = useWakeupText();
  const disable = useDisableIssueWakeup(workspaceId, issueId);
  const enable = useEnableIssueWakeup(workspaceId, issueId);
  // Open issues always show the section so people can add a wakeup.
  if (closed && !data.length && !isError) return null;
  const current = data.filter((w) => isCurrentWakeup(w, wakeupRun(w, tasks)));
  const history = data.filter((w) => !isCurrentWakeup(w, wakeupRun(w, tasks)));
  const row = (wakeup: IssueWakeup) => (
    <WakeupRow
      key={wakeup.id}
      wakeup={wakeup}
      task={wakeupRun(wakeup, tasks)}
      sourceTask={tasks.find((task) => task.id === wakeup.filter_task_id)}
      pending={disable.isPending || enable.isPending}
      closed={closed}
      onEnable={async (input = {}) => {
        await enable.mutateAsync({
          id: wakeup.id,
          revision: wakeup.revision ?? 0,
          ...input,
        });
      }}
      onDisable={() =>
        disable.mutate(wakeup.id, {
          onError: (err) =>
            toast.error(
              text.error(
                err,
                t(($) => $.wakeups.disable_error),
              ),
            ),
          onSuccess: () => setHistoryOpen(true),
        })
      }
    />
  );
  return (
    <section>
      <div className="mb-1 flex items-center gap-1">
        <button
          type="button"
          id={`issue-wakeups-${issueId}`}
          aria-expanded={open}
          onClick={() => setOpen(!open)}
          className="flex min-h-9 flex-1 items-center gap-1 rounded-md px-2 py-1 text-caption font-medium hover:bg-accent/70 focus-visible:outline-2 focus-visible:outline-ring"
        >
          {t(($) => $.wakeups.title)}{" "}
          <span className="text-muted-foreground tabular-nums">
            {current.length + systemRules.length}
          </span>
          <ChevronRight
            className={`size-3 text-muted-foreground ${open ? "rotate-90" : ""}`}
            aria-hidden="true"
          />
        </button>
        {!closed && (
          <WakeupCreate workspaceId={workspaceId} issueId={issueId} defaultAgentId={defaultAgentId} />
        )}
      </div>
      {open && (
        <div>
          {isError && (
            <button
              type="button"
              className="px-2 text-caption text-muted-foreground hover:text-foreground"
              onClick={() => void refetch()}
            >
              {t(($) => $.wakeups.retry)}
            </button>
          )}
          {closed && (
            <p className="px-2 text-caption text-muted-foreground">
              {t(($) => $.wakeups.closed_hint)}
            </p>
          )}
          {!closed &&
            systemRules.map((rule) => (
              <SystemWakeupRow key={rule.rule} rule={rule} workspaceId={workspaceId} issueId={issueId} />
            ))}
          {current.map(row)}
          {history.length > 0 && (
            <>
              <button
                type="button"
                aria-expanded={historyOpen}
                onClick={() => setHistoryOpen(!historyOpen)}
                className="flex min-h-9 w-full items-center gap-1 rounded-md px-2 py-1 text-caption text-muted-foreground hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring"
              >
                <ChevronRight
                  className={`size-3 ${historyOpen ? "rotate-90" : ""}`}
                  aria-hidden="true"
                />
                {t(($) => $.wakeups.ended, { count: history.length })}
              </button>
              {historyOpen && history.map(row)}
            </>
          )}
        </div>
      )}
    </section>
  );
}
