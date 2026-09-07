"use client";

import { useEffect, useId, useMemo, useState, type ReactNode } from "react";
import { AlertCircle, Brain, ChevronRight, ExternalLink, Loader2, MessageSquare, RotateCcw, Square, Terminal } from "lucide-react";
import { toast } from "sonner";
import { useActorName } from "@multica/core/workspace/hooks";
import { useTaskMessages } from "@multica/core/chat/queries";
import { useCancelIssueRun, useRetryIssueRun } from "@multica/core/issues/mutations";
import { dispatchReasonCode } from "@multica/core/api";
import { Card } from "@multica/ui/components/ui/card";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { AgentTranscriptDialog, StepBody } from "../../common/task-transcript/agent-transcript-dialog";
import { buildTimeline } from "../../common/task-transcript/build-timeline";
import { buildSteps, groupSteps, isCallStep, isGroupRow, type TraceRow } from "../../common/task-transcript/build-steps";
import { traceEventSummary, traceToolArgSummary } from "../../common/task-transcript/trace-event-presenter";
import { redactSecrets } from "../../common/task-transcript/redact";
import { ReadonlyContent } from "../../editor";
import { useT } from "../../i18n";
import { formatDuration } from "../../agents/components/agent-activity-hover-content";
import { cancelReasonLabel, failureReasonLabel } from "../../agents/components/tabs/task-failure";
import { TerminateTaskConfirmDialog } from "./terminate-task-confirm-dialog";
import { TaskStatusIcon } from "./task-status-icon";
import { useStatusLabel } from "./task-run-labels";
import { commentRunOutput, isActiveCommentRun, type CommentRun } from "./comment-runs";

import { useRunDisclosureMotion } from "./use-run-comment-motion";

export function useInlineCommentRunState() {
  const [expanded, setExpanded] = useState(false);
  const [fullLogOpen, setFullLogOpen] = useState(false);
  const disclosure = useRunDisclosureMotion(expanded);
  return { expanded, setExpanded, fullLogOpen, setFullLogOpen, disclosure };
}

export type InlineCommentRunState = ReturnType<typeof useInlineCommentRunState>;

export function InlineCommentRun({ run, className, viewState }: { run: CommentRun; className?: string; viewState?: InlineCommentRunState }) {
  const { task, hasReply } = run;
  const { t } = useT("issues");
  const { t: tAgents } = useT("agents");
  const { getActorName } = useActorName();
  const name = getActorName("agent", task.agent_id);
  const status = useStatusLabel(task.status);
  const active = isActiveCommentRun(task);
  const localViewState = useInlineCommentRunState();
  const state = viewState ?? localViewState;
  const { expanded, setExpanded } = state;
  const [confirmStop, setConfirmStop] = useState(false);
  const [now, setNow] = useState(Date.now);
  const cancel = useCancelIssueRun(task.issue_id);
  const retry = useRetryIssueRun(task.issue_id);
  const regionId = useId();
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
  const compact = !active && !failure;
  // Historical, collapsed runs don't fetch transcripts.
  const showActivity = task.status === "running" || expanded;
  const output = !hasReply ? commentRunOutput(task) : null;
  const header = (
    <div className="flex min-w-0 flex-auto flex-wrap items-center gap-x-2.5 gap-y-1">
      <span className="flex items-center gap-1.5 whitespace-nowrap text-caption text-muted-foreground" role="status" data-run-status>
        {task.status === "running"
          ? <span aria-hidden className="size-1.5 rounded-full bg-info" />
          : <TaskStatusIcon status={task.status} />}
        {status}
      </span>
      <span className="whitespace-nowrap text-caption tabular-nums text-muted-foreground">{elapsed}</span>
    </div>
  );
  const controls = (
    <>
      {active && <Button size="sm" variant="ghost" disabled={cancel.isPending || cancel.isSuccess}
        onClick={() => setConfirmStop(true)}>
        {cancel.isPending || cancel.isSuccess ? <Loader2 className="size-3.5 animate-spin motion-reduce:animate-none" /> : <Square className="size-3.5" />}
        {cancel.isPending || cancel.isSuccess ? t(($) => $.inline_run.stopping) : t(($) => $.inline_run.stop)}
      </Button>}
      {!hasReply && (task.status === "failed" || task.status === "cancelled") && <Button
        size="sm" variant="ghost" disabled={retry.isPending || retry.isSuccess}
        onClick={() => retry.mutate(task.id, { onError: (error) => toast.error(
          dispatchReasonCode(error) === "invocation_not_allowed" ? t(($) => $.execution_log.retry_blocked) : t(($) => $.execution_log.retry_failed),
        ) })}>
        <RotateCcw className="size-3.5" />{t(($) => $.execution_log.retry_task_tooltip)}
      </Button>}
    </>
  );
  return (
    <section aria-label={t(($) => $.inline_run.label, { name })}
      className={cn("my-2.5 min-w-0", className)} data-run-id={task.id}>
      {output && <div className="mb-3 text-body"><ReadonlyContent content={redactSecrets(output)} /></div>}
      <Card className="gap-0 rounded-lg border-border/70 bg-card p-0 shadow-none">
        {!hasReply && !compact && <div className="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 pt-3 max-md:px-3">{header}<div className="ml-auto flex items-center">{controls}</div></div>}
        {(failure || ["queued", "dispatched", "waiting_local_directory"].includes(task.status)) && (
          <div className="space-y-2 px-4 py-3 max-md:px-3">
            {failure && <p className="text-caption text-destructive">{failure}</p>}
            {task.status === "queued" && <p className="text-body text-muted-foreground">{t(($) => $.inline_run.queued)}</p>}
            {task.status === "dispatched" && <p className="text-body text-muted-foreground">{t(($) => $.inline_run.starting)}</p>}
            {task.status === "waiting_local_directory" && <p className="text-body text-muted-foreground">{t(($) => $.inline_run.waiting_directory)}</p>}
          </div>
        )}
        {showActivity ? (
          <InlineRunActivity run={run} viewState={state} expanded={expanded} onExpandedChange={setExpanded}
            regionId={regionId} name={name} statusLine={hasReply || compact ? header : undefined} controls={hasReply || compact ? controls : undefined} />
        ) : (
          <div className={cn("flex flex-wrap items-center justify-between gap-x-4 gap-y-2 bg-muted/20 px-4 py-2.5 max-md:px-3", !hasReply && !compact && "border-t border-border/50")}>
            {(hasReply || compact) && header}
            <div className={cn("flex items-center gap-3", (hasReply || compact) && "ml-auto")}>
              {(hasReply || compact) && controls}
              <button type="button" className="flex items-center gap-1.5 rounded text-caption text-muted-foreground hover:text-foreground"
                aria-expanded={false} onClick={(event) => { state.disclosure.onTrigger(event); setExpanded(true); }}>
                <ChevronRight ref={state.disclosure.chevronRef} className="size-3.5" />{t(($) => $.inline_run.view_activity)}
              </button>
            </div>
          </div>
        )}
      </Card>
      <TerminateTaskConfirmDialog open={confirmStop} onOpenChange={setConfirmStop}
        showRunningNote={task.status !== "queued"}
        onConfirm={() => cancel.mutate(task.id, { onError: () => toast.error(t(($) => $.execution_log.cancel_failed)) })} />
    </section>
  );
}

function InlineRunActivity({ run, viewState, expanded, onExpandedChange, regionId, name, statusLine, controls }: {
  run: CommentRun; viewState: InlineCommentRunState; expanded: boolean; onExpandedChange: (value: boolean) => void; regionId: string; name: string; statusLine?: ReactNode; controls?: ReactNode;
}) {
  const { t } = useT("issues");
  const live = isActiveCommentRun(run.task);
  const { data, isPending, isError, refetch } = useTaskMessages(run.task.id, live);
  const items = useMemo(() => buildTimeline(data ?? []), [data]);
  const steps = useMemo(() => buildSteps(items), [items]);
  const rows = useMemo(() => groupSteps(steps), [steps]);
  const { fullLogOpen, setFullLogOpen } = viewState;
  const [visibleCount, setVisibleCount] = useState(12);
  const latest = steps.findLast((step) => step.kind !== "text" || step.item.content?.trim());
  const pendingCall = steps.findLast((step) => isCallStep(step) && !step.result);
  const current = pendingCall ?? latest;
  // Keep the last activity visible after a tool returns, until new progress arrives.
  const summary = current && isCallStep(current)
    ? redactSecrets(traceToolArgSummary(current.call?.input) || current.tool)
    : current?.kind === "text" ? current.item.content
    : current?.kind === "thinking" ? t(($) => $.inline_run.thinking)
    : current?.kind === "error" ? t(($) => $.inline_run.error)
    : t(($) => $.inline_run.waiting_response);
  return <>
    {run.task.status === "running" && !run.hasReply && <p className="mx-4 mb-3 mt-2 min-h-[1lh] truncate text-body text-foreground max-md:mx-3" title={summary}>{summary}</p>}
    <div className={cn("bg-muted/20 px-4 py-2.5 max-md:px-3", !statusLine && "border-t border-border/50")}>
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
        {statusLine}
        <div className={cn("flex items-center gap-3", statusLine && "ml-auto")}>
          {controls}
          <button type="button" className="flex items-center gap-1.5 rounded text-caption text-muted-foreground hover:text-foreground"
            aria-expanded={expanded} aria-controls={expanded ? regionId : undefined} onClick={(event) => { viewState.disclosure.onTrigger(event); onExpandedChange(!expanded); }}>
            <ChevronRight ref={viewState.disclosure.chevronRef} className={cn("size-3.5", expanded && "rotate-90")} />
            {t(($) => $.inline_run.view_activity)}
            {steps.length > 0 && <span className="text-faint-foreground">· {t(($) => $.inline_run.steps, { count: steps.length })}</span>}
          </button>
        </div>
      </div>
      {expanded && <div ref={viewState.disclosure.contentRef} id={regionId} className="mt-3 min-w-0 space-y-1">
        {isPending && <p className="text-caption text-muted-foreground">{t(($) => $.inline_run.loading)}</p>}
        {isError && <div role="alert" className="text-caption text-destructive">{t(($) => $.inline_run.load_failed)}
          <button className="ml-2 underline" type="button" onClick={() => void refetch()}>{t(($) => $.inline_run.try_again)}</button></div>}
        {!isPending && !isError && rows.length === 0 && <p className="text-caption text-muted-foreground">{t(($) => $.inline_run.empty)}</p>}
        {rows.length > visibleCount && <button type="button" className="py-1 text-caption text-muted-foreground hover:text-foreground"
          onClick={() => setVisibleCount((count) => count + 12)}>{t(($) => $.inline_run.show_earlier, { count: rows.length - visibleCount })}</button>}
        {rows.slice(-visibleCount).map((row) => <InlineStep key={row.seq} row={row} live={live} />)}
        <button type="button" className="flex items-center gap-1.5 py-2 text-caption text-muted-foreground hover:text-foreground"
          onClick={() => setFullLogOpen(true)}>{t(($) => $.inline_run.full_log)}<ExternalLink className="size-3" /></button>
      </div>}
    </div>
    {fullLogOpen && <AgentTranscriptDialog open onOpenChange={setFullLogOpen} task={run.task} items={items} agentName={name} isLive={live} />}
  </>;
}

function InlineStep({ row, live }: { row: TraceRow; live: boolean }) {
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
    ? redactSecrets(traceToolArgSummary(row.call?.input) || (row.result ? traceEventSummary(row.result) : "")) || row.tool
    : grouped ? row.tool
    : row.kind === "text" ? t(($) => $.inline_run.message)
    : row.kind === "thinking" ? t(($) => $.inline_run.thinking)
    : t(($) => $.inline_run.error);
  return <details className="min-w-0 text-caption" onToggle={onToggle}>
    <summary onClick={disclosure.onTrigger} className="flex cursor-pointer list-none items-center gap-2 rounded py-1.5 hover:bg-accent/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring [&::-webkit-details-marker]:hidden">
      {pending ? <Loader2 aria-hidden className="size-3.5 shrink-0 animate-spin text-info motion-reduce:animate-none" />
        : <Icon aria-hidden className={cn("size-3.5 shrink-0", error ? "text-destructive" : "text-muted-foreground")} />}
      <span className={cn("min-w-0 flex-1 truncate", error && "text-destructive")} title={summary}>{summary}</span>
      {(grouped || call) && <span className="shrink-0 text-micro text-muted-foreground">
        {grouped ? t(($) => $.inline_run.steps, { count: row.steps.length }) : row.tool}
      </span>}
      <ChevronRight ref={disclosure.chevronRef} aria-hidden className={cn("size-3 shrink-0 text-muted-foreground", open && "rotate-90")} />
    </summary>
    {open && <div ref={disclosure.contentRef} className="min-w-0 space-y-2 overflow-hidden pl-5.5">
      {grouped ? <>
        {row.steps.length > limit && <button type="button" className="py-1 text-muted-foreground" onClick={() => setLimit((value) => value + 12)}>
          {t(($) => $.inline_run.show_earlier, { count: row.steps.length - limit })}</button>}
        {row.steps.slice(-limit).map((step) => <InlineStep key={step.seq} row={step} live={live} />)}
      </> : call ? <>
        {row.call && <StepBody item={row.call} />}
        {row.result && <StepBody item={row.result} />}
        {pending && <p className="text-muted-foreground">{t(($) => $.inline_run.waiting_result)}</p>}
      </> : <StepBody item={row.item} />}
    </div>}
  </details>;
}
