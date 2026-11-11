"use client";

import { useMemo } from "react";
import {
  ArrowUpRight,
  Bot,
  Check,
  ChevronRight,
  CircleAlert,
  Clock,
  Copy,
  FilePen,
  FilePlus,
  GitBranch,
  GitPullRequest,
  MessageSquare,
  Sparkles,
  TriangleAlert,
} from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspacePaths } from "@multica/core/paths";
import type { AgentTask } from "@multica/core/types/agent";
import type { GitHubPullRequest } from "@multica/core/types/github";
import { useNavigation } from "../../navigation";
import { RichContent } from "../../rich-content";
import { useT } from "../../i18n";
import { plainTriggerSummary, useFormatSpend } from "../../issues/components/task-run-labels";
import { formatTokens, hasTokenFigure, type TaskUsageSummary } from "../../runtimes/utils";
import { isCallStep, type TraceCallStep, type TraceLanes, type TraceMessageStep, type TraceStep } from "./build-steps";
import type { RunComment } from "./run-deliverables";
import type { RunOutcome } from "./run-outcome";
import { formatLaneTotal, RunTimelineMini } from "./run-timeline";
import { callSummary, firstLineOf, formatOffset, workdirRelativePath } from "./step-summary";
import type { TraceSummaryLabels } from "./trace-event-presenter";

// The run's answer before its evidence: what it said, what it changed, what it
// delivered, what went wrong, and what it cost. Every item that has a step
// behind it opens that step, so the overview is a way into the evidence, not a
// second copy of it.

interface RunOverviewProps {
  task: AgentTask;
  steps: TraceStep[];
  outcome: RunOutcome | null;
  lanes: TraceLanes | null;
  usage: TaskUsageSummary | null;
  isLive: boolean;
  runStartMs?: number;
  /** Localized failure / cancellation reason, when the run did not complete. */
  reasonLabel: string | null;
  comments: RunComment[];
  pullRequests: GitHubPullRequest[];
  formatText: (text: string) => string;
  onOpenStep: (seq: number) => void;
  onSeek: (offsetMs: number) => void;
  onCopyBranch: () => void;
  copiedBranch: boolean;
  /** Leaving the dialog for a comment closes it. */
  onClose: () => void;
}

function lastOf<T>(items: readonly T[], match: (item: T) => boolean): T | undefined {
  for (let i = items.length - 1; i >= 0; i--) {
    const item = items[i]!;
    if (match(item)) return item;
  }
  return undefined;
}

export function RunOverview({
  task,
  steps,
  outcome,
  lanes,
  usage,
  isLive,
  runStartMs,
  reasonLabel,
  comments,
  pullRequests,
  formatText,
  onOpenStep,
  onSeek,
  onCopyBranch,
  copiedBranch,
  onClose,
}: RunOverviewProps) {
  const { t } = useT("agents");
  const formatSpend = useFormatSpend();
  const summaryLabels = useMemo<TraceSummaryLabels>(
    () => ({
      formatText,
      morePaths: (path, extraCount) =>
        t(($) => $.transcript.patch_summary_more, { path, extra: extraCount }),
    }),
    [t, formatText],
  );

  const offsetOf = (startedAt?: string) => {
    if (!startedAt || runStartMs === undefined) return null;
    const at = new Date(startedAt).getTime();
    return Number.isFinite(at) ? formatOffset(at - runStartMs) : null;
  };

  const lastMessage = lastOf(steps, (step) => step.kind === "text") as TraceMessageStep | undefined;
  // While live, the step still waiting on its result is what the agent is
  // doing right now; between calls the newest step is.
  const currentStep = isLive
    ? (lastOf(steps, (step) => isCallStep(step) && !step.result) ?? steps[steps.length - 1])
    : undefined;
  const errors = steps.filter((step): step is TraceMessageStep => step.kind === "error");
  const longest = steps.reduce<TraceCallStep | undefined>((best, step) => {
    if (!isCallStep(step) || step.durationMs === undefined || step.durationMs <= 0) return best;
    return !best || step.durationMs > (best.durationMs ?? 0) ? step : best;
  }, undefined);

  const files = outcome?.files ?? [];
  const roots = [task.work_dir, task.durable_work_dir];
  const hasDelivered = pullRequests.length > 0 || comments.length > 0 || !!task.branch_name;
  const hasAttention = errors.length > 0 || !!reasonLabel;

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-y-auto md:grid md:grid-cols-[minmax(0,1fr)_20rem] md:overflow-hidden">
      <div className="flex min-w-0 flex-col gap-5 px-5 py-4 md:overflow-y-auto">
        {currentStep && (
          <NowCard
            step={currentStep}
            message={lastMessage}
            label={
              isCallStep(currentStep)
                ? currentStep.tool || t(($) => $.transcript.kind_tool)
                : currentStep.kind === "thinking"
                  ? t(($) => $.transcript.kind_thinking)
                  : currentStep.kind === "error"
                    ? t(($) => $.transcript.kind_error)
                    : t(($) => $.transcript.kind_text)
            }
            summary={isCallStep(currentStep) ? callSummary(currentStep, summaryLabels) : firstLineOf(currentStep.item.content)}
            messageOffset={offsetOf(lastMessage?.startedAt)}
            onOpenStep={onOpenStep}
          />
        )}

        {!isLive && lastMessage && (
          <OverviewSection
            icon={Bot}
            title={t(($) => $.transcript.overview_result)}
            meta={offsetOf(lastMessage.startedAt) && (
              <button
                type="button"
                onClick={() => onOpenStep(lastMessage.seq)}
                className="ml-auto text-micro text-faint-foreground transition-colors hover:text-foreground"
              >
                {t(($) => $.transcript.overview_final_message, { offset: offsetOf(lastMessage.startedAt) })}
              </button>
            )}
          >
            <div className="rounded-lg border bg-surface px-3.5 py-3">
              <RichContent
                content={lastMessage.item.content ?? ""}
                density="compact"
                className="transcript-prose"
              />
            </div>
          </OverviewSection>
        )}

        {files.length > 0 && outcome && (
          <OverviewSection
            icon={FilePen}
            title={t(($) => $.transcript.overview_files)}
            meta={
              <span className="flex items-center gap-1.5 text-micro tabular-nums">
                <span className="text-muted-foreground">{files.length}</span>
                {outcome.addedLines > 0 && <span className="text-success">+{outcome.addedLines}</span>}
                {outcome.removedLines > 0 && <span className="text-destructive">−{outcome.removedLines}</span>}
              </span>
            }
          >
            <ul className="divide-y overflow-hidden rounded-lg border bg-surface">
              {files.map((file) => {
                const shown = workdirRelativePath(file.path, roots);
                const slash = shown.lastIndexOf("/");
                return (
                  <li key={file.path}>
                    <button
                      type="button"
                      onClick={() => onOpenStep(file.lastSeq)}
                      title={file.path}
                      aria-label={t(($) => $.transcript.overview_open_file, { path: shown })}
                      className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-caption transition-colors hover:bg-accent/40"
                    >
                      {file.created ? (
                        <FilePlus aria-hidden className="h-3 w-3 shrink-0 text-muted-foreground" />
                      ) : (
                        <FilePen aria-hidden className="h-3 w-3 shrink-0 text-muted-foreground" />
                      )}
                      <span className="min-w-0 flex-1 truncate font-mono text-micro">
                        {slash > 0 && <span className="text-muted-foreground">{shown.slice(0, slash + 1)}</span>}
                        {shown.slice(slash + 1)}
                      </span>
                      {file.created && (
                        <span className="shrink-0 rounded-xs bg-success/15 px-1.5 text-micro text-success">
                          {t(($) => $.transcript.overview_file_new)}
                        </span>
                      )}
                      <span className="w-12 shrink-0 text-right font-mono text-micro tabular-nums">
                        {file.addedLines > 0 && <span className="text-success">+{file.addedLines}</span>}
                        {file.removedLines > 0 && <span className="ml-1 text-destructive">−{file.removedLines}</span>}
                      </span>
                      <ChevronRight aria-hidden className="h-3 w-3 shrink-0 text-faint-foreground" />
                    </button>
                  </li>
                );
              })}
            </ul>
          </OverviewSection>
        )}

        {!currentStep && !lastMessage && files.length === 0 && (
          <p className="text-body text-muted-foreground">{t(($) => $.transcript.overview_nothing)}</p>
        )}
      </div>

      <div className="flex flex-col gap-4 border-t bg-muted/25 px-4 py-4 md:overflow-y-auto md:border-l md:border-t-0">
        {hasDelivered && (
          <OverviewSection icon={Sparkles} title={t(($) => $.transcript.overview_delivered)}>
            <ul className="divide-y overflow-hidden rounded-lg border bg-surface text-caption">
              {pullRequests.map((pr) => (
                <li key={pr.id}>
                  <a
                    href={pr.html_url}
                    target="_blank"
                    rel="noopener noreferrer"
                    title={t(($) => $.transcript.overview_pr_opened)}
                    className="flex items-center gap-2 px-2.5 py-2 transition-colors hover:bg-accent/40"
                  >
                    <GitPullRequest
                      aria-hidden
                      className={cn("h-3 w-3 shrink-0", pr.state === "open" ? "text-success" : "text-muted-foreground")}
                    />
                    <span className="min-w-0 flex-1 truncate">
                      <span className="font-medium">#{pr.number}</span>{" "}
                      <span className="text-muted-foreground">{pr.title}</span>
                    </span>
                    <ArrowUpRight aria-hidden className="h-3 w-3 shrink-0 text-faint-foreground" />
                  </a>
                </li>
              ))}
              {comments.map((comment) => (
                <li key={comment.id}>
                  <CommentLink issueId={task.issue_id} comment={comment} onNavigate={onClose} />
                </li>
              ))}
              {task.branch_name && (
                <li>
                  <button
                    type="button"
                    onClick={onCopyBranch}
                    title={t(($) => $.transcript.copy_branch)}
                    className="group flex w-full items-center gap-2 px-2.5 py-2 text-left transition-colors hover:bg-accent/40"
                  >
                    <GitBranch aria-hidden className="h-3 w-3 shrink-0 text-muted-foreground" />
                    <span className="min-w-0 flex-1 truncate font-mono text-micro">{task.branch_name}</span>
                    {copiedBranch ? (
                      <Check aria-hidden className="h-3 w-3 shrink-0 text-success" />
                    ) : (
                      <Copy aria-hidden className="h-3 w-3 shrink-0 text-faint-foreground" />
                    )}
                  </button>
                </li>
              )}
            </ul>
          </OverviewSection>
        )}

        {hasAttention && (
          <OverviewSection
            icon={TriangleAlert}
            title={t(($) => $.transcript.overview_attention)}
            meta={<span className="ml-auto text-micro tabular-nums text-faint-foreground">{errors.length + (reasonLabel ? 1 : 0)}</span>}
          >
            <ul className="divide-y overflow-hidden rounded-lg border border-warning/40 bg-warning/5 text-caption">
              {reasonLabel && (
                <li className="flex items-start gap-2 px-2.5 py-2">
                  <CircleAlert aria-hidden className="mt-0.5 h-3 w-3 shrink-0 text-destructive" />
                  <span className="min-w-0 flex-1">{reasonLabel}</span>
                </li>
              )}
              {errors.map((error) => (
                <li key={error.seq}>
                  <button
                    type="button"
                    onClick={() => onOpenStep(error.seq)}
                    className="flex w-full items-start gap-2 px-2.5 py-2 text-left transition-colors hover:bg-accent/40"
                  >
                    <CircleAlert aria-hidden className="mt-0.5 h-3 w-3 shrink-0 text-destructive" />
                    <span className="min-w-0 flex-1 line-clamp-2 break-words">{formatText(firstLineOf(error.item.content))}</span>
                    {offsetOf(error.startedAt) && (
                      <span className="shrink-0 font-mono text-micro tabular-nums text-faint-foreground">
                        {offsetOf(error.startedAt)}
                      </span>
                    )}
                  </button>
                </li>
              ))}
            </ul>
          </OverviewSection>
        )}

        {(lanes || usage || longest) && (
          <OverviewSection
            icon={Clock}
            title={t(($) => $.transcript.overview_time_cost)}
            meta={lanes && (
              <span className="ml-auto text-micro tabular-nums text-muted-foreground">
                {formatLaneTotal(lanes.totalMs)}
              </span>
            )}
          >
            <div className="rounded-lg border bg-surface px-3 py-2.5">
              {lanes && <RunTimelineMini lanes={lanes} onSeek={onSeek} />}
              <dl className={cn("grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-caption", lanes && "mt-2.5")}>
                {lanes && (
                  <>
                    <dt className="text-muted-foreground">{t(($) => $.transcript.lane_model)}</dt>
                    <dd className="tabular-nums">{formatLaneTotal(lanes.modelMs)}</dd>
                    <dt className="text-muted-foreground">{t(($) => $.transcript.lane_tool)}</dt>
                    <dd className="tabular-nums">{formatLaneTotal(lanes.toolMs)}</dd>
                  </>
                )}
                {usage && hasTokenFigure(usage) && (
                  <>
                    <dt className="text-muted-foreground">{t(($) => $.transcript.overview_tokens)}</dt>
                    <dd className="tabular-nums">{formatTokens(usage.tokens)}</dd>
                  </>
                )}
                {usage && (
                  <>
                    <dt className="text-muted-foreground">{t(($) => $.transcript.details_cost)}</dt>
                    <dd className="tabular-nums">{formatSpend(usage)}</dd>
                  </>
                )}
                {longest && (
                  <>
                    <dt className="text-muted-foreground">{t(($) => $.transcript.overview_longest_step)}</dt>
                    <dd className="min-w-0">
                      <button
                        type="button"
                        onClick={() => onOpenStep(longest.seq)}
                        className="flex w-full min-w-0 items-baseline gap-1.5 text-left transition-colors hover:text-brand"
                      >
                        <span className="min-w-0 flex-1 truncate">
                          <span className="font-medium">{longest.tool}</span>{" "}
                          <span className="font-mono text-micro text-muted-foreground">{callSummary(longest, summaryLabels)}</span>
                        </span>
                        <span className="shrink-0 font-mono text-micro tabular-nums text-faint-foreground">
                          {formatLaneTotal(longest.durationMs ?? 0)}
                        </span>
                      </button>
                    </dd>
                  </>
                )}
              </dl>
            </div>
          </OverviewSection>
        )}

        <p className="mt-auto text-micro text-faint-foreground">
          {t(($) => $.transcript.steps_count, { count: steps.length })}
          {outcome && outcome.commandCount > 0 && (
            <> · {t(($) => $.transcript.outcome_commands, { count: outcome.commandCount })}</>
          )}
        </p>
      </div>
    </div>
  );
}

function OverviewSection({
  icon: Icon,
  title,
  meta,
  children,
}: {
  icon: typeof Bot;
  title: string;
  meta?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <section>
      <div className="mb-2 flex items-center gap-1.5">
        <Icon aria-hidden className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <h3 className="text-label font-semibold">{title}</h3>
        {meta}
      </div>
      {children}
    </section>
  );
}

/** What the agent is doing now, and the last thing it said about it. */
function NowCard({
  step,
  message,
  label,
  summary,
  messageOffset,
  onOpenStep,
}: {
  step: TraceStep;
  message?: TraceMessageStep;
  label: string;
  summary: string;
  messageOffset: string | null;
  onOpenStep: (seq: number) => void;
}) {
  const { t } = useT("agents");
  const pending = isCallStep(step) && !step.result;
  return (
    <section
      aria-label={t(($) => $.transcript.overview_now)}
      className="flex items-start gap-2.5 rounded-lg border border-info/30 bg-info/5 px-3 py-2.5"
    >
      <span aria-hidden className="mt-1.5 h-2 w-2 shrink-0 rounded-full bg-info ring-4 ring-info/20" />
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-baseline gap-1.5">
          <span className="shrink-0 text-label font-medium">{label}</span>
          <span className="min-w-0 truncate font-mono text-caption text-muted-foreground">{summary}</span>
          {pending && (
            <span className="ml-auto shrink-0 text-micro text-info">{t(($) => $.transcript.step_running)}</span>
          )}
        </div>
        {message && (
          <p className="mt-1 line-clamp-3 whitespace-pre-wrap break-words text-label">{message.item.content}</p>
        )}
        <div className="mt-1 flex items-center gap-1.5 text-micro text-faint-foreground">
          {message && messageOffset && (
            <span>{t(($) => $.transcript.overview_latest_message, { offset: messageOffset })}</span>
          )}
          <button
            type="button"
            onClick={() => onOpenStep(step.seq)}
            className="text-brand transition-colors hover:underline"
          >
            {t(($) => $.transcript.overview_jump_to_step)}
          </button>
        </div>
      </div>
    </section>
  );
}

function CommentLink({
  issueId,
  comment,
  onNavigate,
}: {
  issueId: string;
  comment: RunComment;
  onNavigate: () => void;
}) {
  const { t: tIssues } = useT("issues");
  const { t } = useT("agents");
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const text = plainTriggerSummary(comment.content, tIssues(($) => $.execution_log.trigger_image));
  return (
    <button
      type="button"
      onClick={() => {
        navigation.push(`${paths.issueDetail(issueId)}#comment-${comment.id}`);
        onNavigate();
      }}
      aria-label={t(($) => $.transcript.overview_open_comment)}
      className="flex w-full items-center gap-2 px-2.5 py-2 text-left transition-colors hover:bg-accent/40"
    >
      <MessageSquare aria-hidden className="h-3 w-3 shrink-0 text-muted-foreground" />
      <span className="min-w-0 flex-1 truncate">{text || t(($) => $.transcript.overview_open_comment)}</span>
      <ChevronRight aria-hidden className="h-3 w-3 shrink-0 text-faint-foreground" />
    </button>
  );
}
