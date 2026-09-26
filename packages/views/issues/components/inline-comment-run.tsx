"use client";

import { useEffect, useId, useMemo, useState, type ReactNode } from "react";
import { AlertCircle, Brain, Check, ChevronRight, CirclePause, Clock3, CornerDownRight, ExternalLink, Loader2, MessageSquare, RotateCcw, ScrollText, Square, Terminal } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useTraceIssueLabels } from "../../common/task-transcript/use-trace-issue-labels";
import { useActorName } from "@multica/core/workspace/hooks";
import { useTaskMessages } from "@multica/core/chat/queries";
import { useCancelIssueRun, useRetryIssueRun } from "@multica/core/issues/mutations";
import { issueTimelineOptions } from "@multica/core/issues/queries";
import { commentSupplementReceipts } from "@multica/core/issues/run-steering";
import { dispatchReasonCode } from "@multica/core/api";
import type { AgentTask, TimelineEntry } from "@multica/core/types";
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
import { useLocale, useT, useTimeAgo } from "../../i18n";
import { formatDuration } from "../../agents/components/agent-activity-hover-content";
import { failureNeedsAction, isCancelledOutcome, runOutcomeLabel } from "../../agents/components/tabs/task-failure";
import { TerminateTaskConfirmDialog } from "./terminate-task-confirm-dialog";
import { TaskStatusIcon } from "./task-status-icon";
import { useStatusLabel } from "./task-run-labels";
import { commentRunOutput, isActiveCommentRun, showCommentRunInHeader, type CommentRun } from "./comment-runs";

import { useRunAnimationVisibility, useRunDisclosureMotion } from "./use-run-comment-motion";

function thinkingPreview(content: string | undefined, formatText: (text: string) => string): string {
  // Redact the complete content before clipping so a split credential cannot leak.
  return traceEventSummary({ type: "thinking", content: redactSecrets(formatText(content ?? "")) });
}

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

export function PlacedInlineCommentRun({ presentation = "inline", ...props }: Parameters<typeof InlineCommentRun>[0]) {
  // A live reply compacts its run into the comment header.
  if ((presentation === "header") !== showCommentRunInHeader(props.run)) return null;
  return <InlineCommentRun {...props} presentation={presentation} />;
}

export function InlineCommentRun({ run, className, viewState, showIdentity = false, presentation = "inline", replyTo, replacesFailureNotice = false }: {
  run: CommentRun;
  className?: string;
  viewState?: InlineCommentRunState;
  showIdentity?: boolean;
  presentation?: "inline" | "header";
  /** The input this run answers, under its identity like a reply's. */
  replyTo?: ReactNode;
  /** The run's reply is only its failure notice, which this block stands in for. */
  replacesFailureNotice?: boolean;
}) {
  const { task, hasReply } = run;
  const { t } = useT("issues");
  const { t: tAgents } = useT("agents");
  const { getActorName } = useActorName();
  const locale = useLocale();
  const timeAgo = useTimeAgo();
  const name = getActorName("agent", task.agent_id);
  const status = useStatusLabel(task.status);
  const ended = task.status === "failed" || task.status === "cancelled";
  const cancelled = isCancelledOutcome(task);
  // One label says how the run ended (MUL-7692). The raw error stays
  // reachable: on hover, or in view when someone has to fix a setting.
  const statusText = (ended ? runOutcomeLabel(task, tAgents) : null) ?? status;
  const rawError = ended && task.error?.trim() ? redactSecrets(task.error.trim()) : "";
  const needsAction = failureNeedsAction(task);
  // Without a reply, how the run ended is the block's content, not metadata.
  const outcomeIsContent = showIdentity && ended;
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
  // Messages that steered this turn, placed in its step list where it read them.
  const { data: steers = NO_STEERS } = useQuery({
    ...issueTimelineOptions(task.issue_id),
    enabled: false,
    select: (entries: TimelineEntry[]) => deliveredSteers(entries, task.id),
  });
  useEffect(() => {
    if (!active) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [active]);
  const start = task.started_at ?? task.dispatched_at ?? task.created_at;
  const end = active ? now : task.completed_at ? Date.parse(task.completed_at) : undefined;
  const elapsed = end !== undefined && Number.isFinite(Date.parse(start)) && Number.isFinite(end)
    ? formatDuration(start, end) : "";
  const output = !hasReply ? commentRunOutput(task) : null;
  const latest = steps.findLast((step) => step.kind !== "text" || step.item.content?.trim());
  const pendingCall = steps.findLast((step) => isCallStep(step) && !step.result);
  const current = pendingCall ?? latest;
  // Keep the last activity visible after a tool returns, until new progress arrives.
  const activitySummary = current && isCallStep(current)
    ? redactSecrets(traceToolArgSummary(current.call?.input, { formatText }) || current.tool)
    : current?.kind === "text" ? redactSecrets(formatText(current.item.content ?? ""))
    : current?.kind === "thinking" ? thinkingPreview(current.item.content, formatText) || t(($) => $.inline_run.thinking)
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
          <ActorAvatar actorType="agent" actorId={task.agent_id} size="md" enableHoverCard showStatusDot />
          <span className="max-w-[30%] shrink-0 truncate text-body font-medium" title={name}>{name}</span>
          {!active && task.completed_at && <Tooltip>
            <TooltipTrigger render={<span className="shrink-0 cursor-default text-caption text-muted-foreground @max-[32rem]/run:hidden">
              {timeAgo(task.completed_at)}
            </span>} />
            <TooltipContent side="top">{new Date(task.completed_at).toLocaleString(locale)}</TooltipContent>
          </Tooltip>}
        </>}
        <span className={cn("flex min-w-0 items-center gap-1.5 whitespace-nowrap",
          outcomeIsContent ? "text-body text-foreground" : "max-w-[50%] shrink-0 text-caption text-muted-foreground",
          showProgress && "sr-only")}
          role="status" data-run-status>
          <TaskStatusIcon status={cancelled ? "cancelled" : task.status} />
          {rawError && rawError !== statusText && !needsAction ? <Tooltip>
            <TooltipTrigger render={<span className="truncate">{statusText}</span>} />
            <TooltipContent side="top" className="flex-col items-start">
              <span>{statusText}</span>
              <span className="break-all font-mono text-muted-foreground">{rawError}</span>
            </TooltipContent>
          </Tooltip> : <span className="truncate" title={statusText}>{statusText}</span>}
        </span>
        <button type="button"
          className={cn("flex min-w-0 items-center gap-1.5 rounded-xs py-1 text-left text-caption text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
            showProgress ? "flex-1 text-body" : "ml-auto shrink-0",
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
        {(!hasReply || replacesFailureNotice) && ended && <Button
          size="xs" variant="outline" className={cn(showIdentity && "@max-[32rem]/run:size-6 @max-[32rem]/run:p-0")} disabled={retry.isPending || retry.isSuccess}
          onClick={() => retry.mutate(task.id, { onError: (error) => toast.error(
            dispatchReasonCode(error) === "invocation_not_allowed" ? t(($) => $.execution_log.retry_blocked) : t(($) => $.execution_log.retry_failed),
          ) })}>
          <RotateCcw /><span className={cn(showIdentity && "@max-[32rem]/run:sr-only")}>{t(($) => $.execution_log.retry_task_tooltip)}</span>
        </Button>}
      </div>
      <div className={cn(showIdentity && "pl-8")}>
        {replyTo}
        {output && <div className="mt-2 text-body"><ReadonlyContent content={redactSecrets(output)} /></div>}
        {needsAction && rawError && <p title={rawError}
          className="mt-1.5 line-clamp-4 whitespace-pre-wrap break-words rounded-md border bg-muted/50 px-2.5 py-1.5 font-mono text-caption text-muted-foreground">
          {rawError}
        </p>}
        {expanded && <div id={regionId} className="mt-2 min-w-0 space-y-1">
          {isPending && <p className="text-caption text-muted-foreground">{t(($) => $.inline_run.loading)}</p>}
          {isError && <div role="alert" className="text-caption text-destructive">{t(($) => $.inline_run.load_failed)}
            <button className="ml-2 underline" type="button" onClick={() => void refetch()}>{t(($) => $.inline_run.try_again)}</button></div>}
          {!isPending && !isError && rows.length === 0 && <p className="text-caption text-muted-foreground">{t(($) => $.inline_run.empty)}</p>}
          {rows.length > visibleCount && <button type="button" className="py-1 text-caption text-muted-foreground hover:text-foreground"
            onClick={() => setVisibleCount((count) => count + 12)}>{t(($) => $.inline_run.show_earlier, { count: rows.length - visibleCount })}</button>}
          {interleaveSteers(rows.slice(-visibleCount), steers, rows.length > visibleCount).map((item) => "steer" in item
            ? <InlineSteer key={`steer:${item.steer.id}`} steer={item.steer} />
            : <InlineStep key={item.row.seq} row={item.row} live={active} formatText={formatText} />)}
          <button type="button" className="flex items-center gap-1.5 rounded-xs py-2 text-caption text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            onClick={openFullLog}>{t(($) => $.inline_run.full_log)}<ExternalLink className="size-3" /></button>
        </div>}
      </div>
      {transcript}
      {stopDialog}
    </section>
  );
}

interface DeliveredSteer {
  id: string;
  actorType: string;
  actorId: string;
  content: string;
  deliveredAt: string;
}

const NO_STEERS: DeliveredSteer[] = [];

function deliveredSteers(entries: TimelineEntry[], taskId: string): DeliveredSteer[] {
  const out: DeliveredSteer[] = [];
  for (const entry of entries) {
    if (entry.type !== "comment") continue;
    const receipt = commentSupplementReceipts(entry)
      .find((r) => r.task_id === taskId && r.status === "delivered" && r.delivered_at);
    if (!receipt) continue;
    out.push({ id: entry.id, actorType: entry.actor_type, actorId: entry.actor_id, content: entry.content ?? "", deliveredAt: receipt.delivered_at! });
  }
  return out.length ? out : NO_STEERS;
}

function rowStartedAt(row: TraceRow): number {
  const at = isGroupRow(row) ? row.steps[0]?.startedAt : row.startedAt;
  return at ? Date.parse(at) : Number.NaN;
}

// A steer sits right before the first step that started after the turn read
// it. With earlier steps folded away, a steer read before the first visible
// step folds away with them.
function interleaveSteers(rows: TraceRow[], steers: DeliveredSteer[], hasEarlier: boolean): ({ row: TraceRow } | { steer: DeliveredSteer })[] {
  if (steers.length === 0) return rows.map((row) => ({ row }));
  const pending = [...steers].sort((a, b) => Date.parse(a.deliveredAt) - Date.parse(b.deliveredAt));
  const firstVisible = rows.length ? rowStartedAt(rows[0]!) : Number.NaN;
  const out: ({ row: TraceRow } | { steer: DeliveredSteer })[] = [];
  let next = 0;
  if (hasEarlier && Number.isFinite(firstVisible)) {
    while (next < pending.length && Date.parse(pending[next]!.deliveredAt) < firstVisible) next++;
  }
  for (const row of rows) {
    const started = rowStartedAt(row);
    while (next < pending.length && Number.isFinite(started) && Date.parse(pending[next]!.deliveredAt) < started) {
      out.push({ steer: pending[next++]! });
    }
    out.push({ row });
  }
  while (next < pending.length) out.push({ steer: pending[next++]! });
  return out;
}

function InlineSteer({ steer }: { steer: DeliveredSteer }) {
  const { t } = useT("issues");
  const { getActorName } = useActorName();
  const name = getActorName(steer.actorType, steer.actorId);
  const summary = traceEventSummary({ type: "text", content: steer.content });
  return <div className="flex min-w-0 items-center gap-2 rounded-xs bg-brand/7 px-1 py-1.5 text-caption dark:bg-brand/12" data-steer-comment={steer.id}>
    <CornerDownRight aria-hidden className="size-3.5 shrink-0 text-brand" />
    <span className="min-w-0 flex-1 truncate" title={steer.content}>
      <span className="font-medium">{t(($) => $.inline_run.steer_step, { name })}</span>
      <span className="text-muted-foreground"> · {summary}</span>
    </span>
    <span className="inline-flex shrink-0 items-center gap-0.5 text-micro text-success">
      <Check aria-hidden className="size-3" />{t(($) => $.inline_run.steer_step_read)}
    </span>
  </div>;
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
  const previewCall = grouped ? row.steps[0] : call ? row : undefined;
  const toolSummary = previewCall
    ? redactSecrets(traceToolArgSummary(previewCall.call?.input, { formatText }) || (previewCall.result ? traceEventSummary(previewCall.result, { formatText }) : ""))
    : "";
  const summary = call
    ? toolSummary || row.tool
    : grouped ? toolSummary ? `${row.tool} · ${toolSummary}` : row.tool
    : row.kind === "text" ? traceEventSummary({ ...row.item, content: redactSecrets(formatText(row.item.content ?? "")) }) || t(($) => $.inline_run.message)
    : row.kind === "thinking" ? thinkingPreview(row.item.content, formatText) || t(($) => $.inline_run.thinking)
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
