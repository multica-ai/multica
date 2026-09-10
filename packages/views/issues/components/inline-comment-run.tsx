"use client";

import { useEffect, useId, useMemo, useState } from "react";
import { AlertCircle, Brain, ChevronRight, CirclePause, Clock3, ExternalLink, Loader2, MessageSquare, RotateCcw, ScrollText, Square, Terminal } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useTraceIssueLabels } from "../../common/task-transcript/use-trace-issue-labels";
import { useActorName } from "@multica/core/workspace/hooks";
import { useTaskMessages } from "@multica/core/chat/queries";
import { useCancelIssueRun, useRetryIssueRun } from "@multica/core/issues/mutations";
import { dispatchReasonCode } from "@multica/core/api";
import type { AgentTask } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { Button } from "@multica/ui/components/ui/button";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { AgentTranscriptDialog, StepBody } from "../../common/task-transcript/agent-transcript-dialog";
import { buildTimeline } from "../../common/task-transcript/build-timeline";
import { buildSteps, groupSteps, isCallStep, isGroupRow, type TraceRow } from "../../common/task-transcript/build-steps";
import { traceEventSummary, traceToolArgSummary } from "../../common/task-transcript/trace-event-presenter";
import { redactSecrets } from "../../common/task-transcript/redact";
import { ReadonlyContent } from "../../editor";
import { useT } from "../../i18n";
import { formatDuration } from "../../agents/components/agent-activity-hover-content";
import { cancellationActorLabel, cancelReasonLabel, failureReasonLabel } from "../../agents/components/tabs/task-failure";
import { TerminateTaskConfirmDialog } from "./terminate-task-confirm-dialog";
import { TaskStatusIcon } from "./task-status-icon";
import { useStatusLabel } from "./task-run-labels";
import { commentRunOutput, isActiveCommentRun, showCommentRunInHeader, type CommentRun } from "./comment-runs";

import { useRunAnimationVisibility, useRunDisclosureMotion } from "./use-run-comment-motion";

export function useInlineCommentRunState() {
  const [expanded, setExpanded] = useState(false);
  const [fullLogOpen, setFullLogOpen] = useState(false);
  // A click carrying no detail count came from Enter/Space. Only that reader
  // needs focus handed back when the log closes; giving it back after a
  // pointer open is what leaves this trigger ringed and tooltipped on Esc.
  const [logFromKeyboard, setLogFromKeyboard] = useState(false);
  const disclosure = useRunDisclosureMotion(expanded);
  const openFullLog = (event: React.MouseEvent<HTMLElement>) => {
    setLogFromKeyboard(event.detail === 0);
    setFullLogOpen(true);
  };
  return { expanded, setExpanded, fullLogOpen, setFullLogOpen, openFullLog, logFromKeyboard, disclosure };
}

export type InlineCommentRunState = ReturnType<typeof useInlineCommentRunState>;

export function InlineCommentRun({ run, className, viewState, showIdentity = false, presentation = "inline" }: {
  run: CommentRun;
  className?: string;
  viewState?: InlineCommentRunState;
  showIdentity?: boolean;
  presentation?: "inline" | "header";
}) {
  const { task, hasReply } = run;
  const { t } = useT("issues");
  const { t: tAgents } = useT("agents");
  const { getActorName } = useActorName();
  const name = getActorName("agent", task.agent_id);
  const status = useStatusLabel(task.status);
  const statusText = cancellationActorLabel(task, tAgents) ?? status;
  const active = isActiveCommentRun(task);
  const localViewState = useInlineCommentRunState();
  const state = viewState ?? localViewState;
  const { expanded, setExpanded, fullLogOpen, setFullLogOpen, openFullLog, logFromKeyboard } = state;
  const [confirmStop, setConfirmStop] = useState(false);
  const [now, setNow] = useState(Date.now);
  const [visibleCount, setVisibleCount] = useState(12);
  const animationVisibility = useRunAnimationVisibility<HTMLDivElement>();
  const cancel = useCancelIssueRun(task.issue_id);
  const retry = useRetryIssueRun(task.issue_id);
  const regionId = useId();
  // Keep one disclosure button mounted across queued, live, and historical states.
  // Historical, collapsed runs still don't fetch transcripts.
  const loadTranscript = task.status === "running" || (presentation === "inline" && expanded) || fullLogOpen;
  const { data, isPending, isError, refetch } = useTaskMessages(task.id, active, loadTranscript);
  const items = useMemo(() => buildTimeline(data ?? []), [data]);
  const formatText = useTraceIssueLabels(useWorkspaceId(), task.issue_id, items, loadTranscript);
  const steps = useMemo(() => buildSteps(items), [items]);
  const rows = useMemo(() => groupSteps(steps), [steps]);
  useEffect(() => {
    if (!active) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [active]);
  const start = task.started_at ?? task.dispatched_at ?? task.created_at;
  const end = active ? now : task.completed_at ? Date.parse(task.completed_at) : undefined;
  const elapsed = end !== undefined && Number.isFinite(Date.parse(start)) && Number.isFinite(end)
    ? formatDuration(start, end) : "";
  const failure = task.status === "failed"
    ? failureReasonLabel(task.failure_reason, tAgents)
    : cancelReasonLabel(task, tAgents);
  const output = !hasReply ? commentRunOutput(task) : null;
  const latest = steps.findLast((step) => step.kind !== "text" || step.item.content?.trim());
  const pendingCall = steps.findLast((step) => isCallStep(step) && !step.result);
  const current = pendingCall ?? latest;
  // Keep the last activity visible after a tool returns, until new progress arrives.
  const activitySummary = current && isCallStep(current)
    ? redactSecrets(traceToolArgSummary(current.call?.input, { formatText }) || current.tool)
    : current?.kind === "text" ? redactSecrets(formatText(current.item.content ?? ""))
    : current?.kind === "thinking" ? t(($) => $.inline_run.thinking)
    : current?.kind === "error" ? t(($) => $.inline_run.error)
    : t(($) => $.inline_run.waiting_response);
  const summary = task.status === "queued" ? t(($) => $.inline_run.queued)
    : task.status === "dispatched" ? t(($) => $.inline_run.starting)
    : task.status === "waiting_local_directory" ? t(($) => $.inline_run.waiting_directory)
    : activitySummary;
  const showProgress = active && !hasReply;
  const activityLabel = t(($) => $.inline_run.view_activity);
  const stepLabel = steps.length > 0 ? t(($) => $.inline_run.steps, { count: steps.length }) : "";
  const stopLabel = cancel.isPending || cancel.isSuccess ? t(($) => $.inline_run.stopping) : t(($) => $.inline_run.stop);
  const transcript = fullLogOpen && <AgentTranscriptDialog open onOpenChange={setFullLogOpen}
    task={task} items={items} agentName={name} isLive={active} finalFocus={logFromKeyboard}
    contentState={isPending ? <p role="status" className="text-body text-muted-foreground">{t(($) => $.inline_run.loading)}</p>
      : isError ? <div role="alert" className="text-body text-destructive">{t(($) => $.inline_run.load_failed)}
        <button className="ml-2 underline" type="button" onClick={() => void refetch()}>{t(($) => $.inline_run.try_again)}</button>
      </div> : undefined} />;
  const stopButton = active && <Button size="icon-sm" variant="ghost" className="text-muted-foreground"
    aria-label={stopLabel} title={stopLabel} disabled={cancel.isPending || cancel.isSuccess}
    onClick={() => setConfirmStop(true)}>
    {cancel.isPending || cancel.isSuccess ? <Loader2 className="size-3.5 animate-spin motion-reduce:animate-none" /> : <Square className="size-3.5" />}
  </Button>;
  const stopDialog = <TerminateTaskConfirmDialog open={confirmStop} onOpenChange={setConfirmStop}
    showRunningNote={task.status !== "queued"}
    onConfirm={() => cancel.mutate(task.id, { onError: () => toast.error(t(($) => $.execution_log.cancel_failed)) })} />;
  if (presentation === "header" && showCommentRunInHeader(run)) {
    return <span className="inline-flex shrink-0" data-comment-actions data-run-id={task.id}>
      <Tooltip>
        <TooltipTrigger render={<Button type="button" size="icon-sm" variant="ghost"
          className="text-muted-foreground aria-expanded:bg-transparent aria-expanded:hover:bg-muted dark:aria-expanded:hover:bg-muted/50"
          aria-label={t(($) => $.inline_run.full_log)} aria-haspopup="dialog" aria-expanded={fullLogOpen}
          onClick={openFullLog}>
          <ScrollText aria-hidden className="size-3.5" />
        </Button>} />
        <TooltipContent>{t(($) => $.inline_run.full_log)}</TooltipContent>
      </Tooltip>
      {transcript}
    </span>;
  }
  return (
    <section aria-label={t(($) => $.inline_run.label, { name })}
      className={cn("@container/run min-w-0 py-2", className)} data-run-id={task.id}>
      <div ref={animationVisibility.ref} className="flex min-h-7 min-w-0 items-center gap-2" data-run-summary-row>
        {showIdentity && <>
          <ActorAvatar actorType="agent" actorId={task.agent_id} size="md" enableHoverCard />
          <span className="max-w-[30%] shrink-0 truncate text-body font-medium" title={name}>{name}</span>
        </>}
        <span className={cn("flex min-w-0 max-w-[50%] shrink-0 items-center gap-1.5 whitespace-nowrap text-caption text-muted-foreground", showProgress && "sr-only")}
          role="status" data-run-status>
          <TaskStatusIcon status={task.status} /><span className="truncate" title={statusText}>{statusText}</span>
        </span>
        <button type="button"
          className={cn("flex min-w-0 items-center gap-1.5 rounded-xs py-1 text-left text-caption text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
            showProgress ? "flex-1 text-body" : "order-last ml-auto shrink-0",
            showIdentity && !showProgress && "@max-[32rem]/run:min-w-7 @max-[32rem]/run:justify-center")}
          aria-label={stepLabel ? `${activityLabel} · ${stepLabel}` : activityLabel}
          aria-expanded={expanded} aria-controls={expanded ? regionId : undefined}
          onClick={(event) => { state.disclosure.onTrigger(event); setExpanded(!expanded); }}>
          {showProgress
            ? <><RunActivityIndicator status={task.status} animate={animationVisibility.visible} />
                <RunActivitySummary summary={summary} /></>
            : <span className={cn(showIdentity && "@max-[32rem]/run:sr-only")}>{activityLabel}</span>}
          {!showProgress && stepLabel && <span className="text-faint-foreground @max-[32rem]/run:hidden">· {stepLabel}</span>}
          <ChevronRight ref={state.disclosure.chevronRef} aria-hidden className={cn("size-3.5 shrink-0", expanded && "rotate-90")} />
        </button>
        <span className={cn("shrink-0 whitespace-nowrap text-caption tabular-nums text-muted-foreground", showIdentity && !active && "@max-[32rem]/run:hidden")}>{elapsed}</span>
        {stopButton}
        {!hasReply && (task.status === "failed" || task.status === "cancelled") && <Button
          size="sm" variant="ghost" className={cn("text-muted-foreground", showIdentity && "@max-[32rem]/run:size-7 @max-[32rem]/run:p-0")} disabled={retry.isPending || retry.isSuccess}
          onClick={() => retry.mutate(task.id, { onError: (error) => toast.error(
            dispatchReasonCode(error) === "invocation_not_allowed" ? t(($) => $.execution_log.retry_blocked) : t(($) => $.execution_log.retry_failed),
          ) })}>
          <RotateCcw className="size-3.5" /><span className={cn(showIdentity && "@max-[32rem]/run:sr-only")}>{t(($) => $.execution_log.retry_task_tooltip)}</span>
        </Button>}
      </div>
      <div className={cn(showIdentity && "pl-8")}>
        {output && <div className="mt-2 text-body"><ReadonlyContent content={redactSecrets(output)} /></div>}
        {failure && <p className="mt-1 text-caption text-destructive">{failure}</p>}
        {expanded && <div id={regionId} className="mt-2 min-w-0 space-y-1">
          {isPending && <p className="text-caption text-muted-foreground">{t(($) => $.inline_run.loading)}</p>}
          {isError && <div role="alert" className="text-caption text-destructive">{t(($) => $.inline_run.load_failed)}
            <button className="ml-2 underline" type="button" onClick={() => void refetch()}>{t(($) => $.inline_run.try_again)}</button></div>}
          {!isPending && !isError && rows.length === 0 && <p className="text-caption text-muted-foreground">{t(($) => $.inline_run.empty)}</p>}
          {rows.length > visibleCount && <button type="button" className="py-1 text-caption text-muted-foreground hover:text-foreground"
            onClick={() => setVisibleCount((count) => count + 12)}>{t(($) => $.inline_run.show_earlier, { count: rows.length - visibleCount })}</button>}
          {rows.slice(-visibleCount).map((row) => <InlineStep key={row.seq} row={row} live={active} formatText={formatText} />)}
          <button type="button" className="flex items-center gap-1.5 rounded-xs py-2 text-caption text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            onClick={openFullLog}>{t(($) => $.inline_run.full_log)}<ExternalLink className="size-3" /></button>
        </div>}
      </div>
      {transcript}
      {stopDialog}
    </section>
  );
}

function InlineStep({ row, live, formatText }: { row: TraceRow; live: boolean; formatText: (text: string) => string }) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(false);
  const disclosure = useRunDisclosureMotion(open);
  const [limit, setLimit] = useState(12);
  const onToggle = (event: React.SyntheticEvent<HTMLDetailsElement>) => setOpen(event.currentTarget.open);
  const grouped = isGroupRow(row);
  const call = isCallStep(row);
  const pending = call && live && !row.result;
  const error = !grouped && !call && row.kind === "error";
  const Icon = grouped || call ? Terminal : row.kind === "text" ? MessageSquare : row.kind === "thinking" ? Brain : AlertCircle;
  const summary = call
    ? redactSecrets(traceToolArgSummary(row.call?.input, { formatText }) || (row.result ? traceEventSummary(row.result, { formatText }) : "")) || row.tool
    : grouped ? row.tool
    : row.kind === "text" ? t(($) => $.inline_run.message)
    : row.kind === "thinking" ? t(($) => $.inline_run.thinking)
    : t(($) => $.inline_run.error);
  return <details className="min-w-0 text-caption" onToggle={onToggle}>
    <summary onClick={disclosure.onTrigger} className="flex cursor-pointer list-none items-center gap-2 rounded-xs py-1.5 hover:bg-accent/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring [&::-webkit-details-marker]:hidden">
      {pending ? <Loader2 aria-hidden className="size-3.5 shrink-0 animate-spin text-info motion-reduce:animate-none" />
        : <Icon aria-hidden className={cn("size-3.5 shrink-0", error ? "text-destructive" : "text-muted-foreground")} />}
      <span className={cn("min-w-0 flex-1 truncate", error && "text-destructive")} title={summary}>{summary}</span>
      {(grouped || call) && <span className="shrink-0 text-micro text-muted-foreground">
        {grouped ? t(($) => $.inline_run.steps, { count: row.steps.length }) : row.tool}
      </span>}
      <ChevronRight ref={disclosure.chevronRef} aria-hidden className={cn("size-3 shrink-0 text-muted-foreground", open && "rotate-90")} />
    </summary>
    {open && <div className="min-w-0 space-y-2 overflow-hidden pl-5.5">
      {grouped ? <>
        {row.steps.length > limit && <button type="button" className="py-1 text-muted-foreground" onClick={() => setLimit((value) => value + 12)}>
          {t(($) => $.inline_run.show_earlier, { count: row.steps.length - limit })}</button>}
        {row.steps.slice(-limit).map((step) => <InlineStep key={step.seq} row={step} live={live} formatText={formatText} />)}
      </> : call ? <>
        {row.call && <StepBody item={row.call} />}
        {row.result && <StepBody item={row.result} />}
        {pending && <p className="text-muted-foreground">{t(($) => $.inline_run.waiting_result)}</p>}
      </> : <StepBody item={row.item} />}
    </div>}
  </details>;
}

function RunActivityIndicator({ status, animate }: { status: AgentTask["status"]; animate: boolean }) {
  if (status === "running" || status === "dispatched") {
    return <Loader2 aria-hidden data-run-loading-indicator className={cn(
      "size-3.5 shrink-0 stroke-[2.25] text-info",
      animate && "motion-safe:animate-spin motion-safe:[animation-duration:900ms]",
    )} />;
  }
  if (status === "waiting_local_directory") {
    return <CirclePause aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />;
  }
  return <Clock3 aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />;
}

function RunActivitySummary({ summary }: { summary: string }) {
  // Update the existing text node. Replacing it for every streamed message
  // invalidates ancestor :has() styles across the entire issue page.
  return <span data-run-summary className="grid h-[1lh] min-w-0 flex-1 overflow-hidden" title={summary}>
    <span className="col-start-1 row-start-1 block min-w-0 max-w-full truncate">{summary}</span>
  </span>;
}
