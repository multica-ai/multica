"use client";

// Phase 7a + 7b — Release detail page.
//
// Shared between web and desktop via `<ShipReleasePage releaseId={…} />`.
// Renders:
//   * Header: title, stage badge, risk badge, project, created-by
//   * Stage progress bar (8 visual stages, current highlighted)
//   * PR table with per-PR remove (assembling stage only) and per-PR
//     merge_state pills (queued / merging / merged / failed / skipped)
//   * Linked channel + issue chips
//   * Timeline (release_event log)
//   * Action bar — context-sensitive to stage:
//       assembling → Edit / Add PR / Cancel / Start merge train
//       merging (running) → progress chip + Abort
//       merging (paused) → paused banner + Resume / Skip & resume / Abort
//       in_staging+ → Phase 7c+ controls (none here)

import { useMemo, useState } from "react";
import { toast } from "sonner";
import {
  AlertTriangle,
  CheckCircle2,
  CircleDashed,
  Hash,
  Loader2,
  MessagesSquare,
  Pause,
  Play,
  Rocket,
  Train,
  X,
  XCircle,
  SkipForward,
} from "lucide-react";
import {
  useReleaseDetail,
  useCancelRelease,
  useRemovePullRequestFromRelease,
  useStartMergeTrain,
  useResumeMergeTrain,
  useAbortMergeTrain,
} from "@multica/core/ship";
import { useCurrentWorkspace } from "@multica/core/paths";
import type { ReleasePullRequest } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label } from "@multica/ui/components/ui/label";
import { cn } from "@multica/ui/lib/utils";
import { PageHeader } from "../../layout/page-header";
import { useT } from "../../i18n";
import { AppLink } from "../../navigation";

// Stage order for the progress bar. Mirrors the Postgres release_stage
// enum, but cancelled / rolled_back are NOT on the bar — they're
// terminal sidetrackers, rendered as a separate "off-track" badge.
const STAGE_PROGRESS: Array<{ key: string; iconBg: string }> = [
  { key: "assembling", iconBg: "bg-muted" },
  { key: "merging", iconBg: "bg-amber-500/20" },
  { key: "in_staging", iconBg: "bg-blue-500/20" },
  { key: "verifying", iconBg: "bg-purple-500/20" },
  { key: "promoting", iconBg: "bg-orange-500/20" },
  { key: "in_production", iconBg: "bg-emerald-500/20" },
  { key: "done", iconBg: "bg-emerald-500/40" },
];

interface ShipReleasePageProps {
  releaseId: string;
}

export function ShipReleasePage({ releaseId }: ShipReleasePageProps) {
  const { t, i18n } = useT("ship");
  const workspace = useCurrentWorkspace();
  const { data, isLoading, isError } = useReleaseDetail(releaseId, true);
  const cancelMutation = useCancelRelease(releaseId);
  const removePR = useRemovePullRequestFromRelease(releaseId);
  const startMerge = useStartMergeTrain(releaseId);
  const resumeMerge = useResumeMergeTrain(releaseId);
  const abortMerge = useAbortMergeTrain(releaseId);
  const [cancelOpen, setCancelOpen] = useState(false);
  const [cancelReason, setCancelReason] = useState("");
  const [abortOpen, setAbortOpen] = useState(false);
  const [abortReason, setAbortReason] = useState("");
  const [resumeOpen, setResumeOpen] = useState(false);

  // Compute "first failed PR error message" for the paused banner.
  // Done as a useMemo so the same value drives both the banner copy
  // and the resume dialog default. Memoized rather than inline so a
  // re-render with no PR shape change doesn't re-walk the array.
  const firstFailureError = useMemo(() => {
    const failed = data?.pull_requests.find((pr) => pr.merge_state === "failed");
    return failed?.merge_error ?? "";
  }, [data?.pull_requests]);

  const failedPRs = useMemo(
    () => (data?.pull_requests ?? []).filter((pr) => pr.merge_state === "failed"),
    [data?.pull_requests],
  );

  if (isLoading) {
    return (
      <div className="flex h-full flex-col">
        <PageHeader>
          <Rocket className="size-4 text-muted-foreground" />
          <h1 className="ml-2 text-sm font-medium">…</h1>
        </PageHeader>
        <div className="space-y-4 p-5">
          <Skeleton className="h-8 w-64" />
          <Skeleton className="h-12 w-full" />
          <Skeleton className="h-32 w-full" />
        </div>
      </div>
    );
  }

  if (isError || !data?.release.id) {
    return (
      <div className="flex h-full flex-col">
        <PageHeader>
          <Rocket className="size-4 text-muted-foreground" />
        </PageHeader>
        <p className="p-5 text-sm text-muted-foreground">
          {t(($) => $.releases.detail.no_prs)}
        </p>
      </div>
    );
  }

  const release = data.release;
  const isAssembling = release.stage === "assembling";
  const isMerging = release.stage === "merging";
  const isPaused = isMerging && release.merge_paused === true;
  const isMergingActive = isMerging && !isPaused;
  const isCancelled = release.stage === "cancelled";
  const isRolledBack = release.stage === "rolled_back";
  const slug = workspace?.slug ?? "";

  // Counts for the merge progress badge. Computed off the PR rows
  // because the release row only carries pr_count (total).
  const totalPRs = data.pull_requests.length;
  const mergedCount = data.pull_requests.filter((pr) => pr.merge_state === "merged").length;
  const inFlight = data.pull_requests.find((pr) => pr.merge_state === "merging");

  const startMergePreconditions = checkStartMergePreconditions(data);
  const canStartMerge = isAssembling && startMergePreconditions.length === 0;

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="px-5">
        <Train className="size-4 text-primary" />
        <h1 className="ml-2 text-sm font-medium">{release.title}</h1>
      </PageHeader>

      <div className="flex-1 overflow-y-auto">
        <div className="space-y-6 p-5">
          {/* Header summary row. Risk + stage + creation timestamp. */}
          <header className="flex flex-wrap items-center gap-3">
            <StageBadge stage={release.stage} />
            <RiskPill risk={release.risk_level} />
            <span className="text-xs text-muted-foreground">
              {release.pr_count} PR{release.pr_count === 1 ? "" : "s"}
            </span>
            {isMerging && (
              <span className="text-xs font-medium text-foreground">
                {t(($) => $.releases.merge.progress_inline, {
                  merged: mergedCount,
                  total: totalPRs,
                })}
              </span>
            )}
            {release.approver_id && (
              <span className="text-xs text-muted-foreground">
                {t(($) => $.releases.detail.approver_label)}:{" "}
                <code className="text-foreground">{release.approver_id.slice(0, 8)}</code>
              </span>
            )}
            <div className="ml-auto flex items-center gap-2">
              {isAssembling && (
                <>
                  <Button
                    size="sm"
                    onClick={async () => {
                      try {
                        await startMerge.mutateAsync({});
                        toast.success(t(($) => $.releases.merge.started_toast));
                      } catch (err) {
                        toast.error(err instanceof Error ? err.message : String(err));
                      }
                    }}
                    disabled={!canStartMerge || startMerge.isPending}
                    title={
                      !canStartMerge
                        ? t(($) => $.releases.merge.start_disabled_tooltip)
                        : undefined
                    }
                    data-testid="release-start-merge-button"
                  >
                    <Train className="size-3.5" />
                    {t(($) => $.releases.merge.start_button)}
                  </Button>
                  <Button
                    size="sm"
                    variant="destructive"
                    onClick={() => setCancelOpen(true)}
                    data-testid="release-cancel-button"
                  >
                    <X className="size-3.5" />
                    {t(($) => $.releases.detail.cancel_release)}
                  </Button>
                </>
              )}
              {isMergingActive && (
                <>
                  <span
                    className="inline-flex items-center gap-1.5 text-xs text-muted-foreground"
                    data-testid="release-merging-progress"
                  >
                    <Loader2 className="size-3 animate-spin" />
                    {inFlight ? (
                      t(($) => $.releases.merge.in_flight, { number: inFlight.number })
                    ) : (
                      t(($) => $.releases.merge.in_progress, {
                        merged: mergedCount,
                        total: totalPRs,
                      })
                    )}
                  </span>
                  <Button
                    size="sm"
                    variant="destructive"
                    onClick={() => setAbortOpen(true)}
                    data-testid="release-abort-button"
                  >
                    {t(($) => $.releases.merge.abort_button)}
                  </Button>
                </>
              )}
              {isPaused && (
                <>
                  <Button
                    size="sm"
                    onClick={async () => {
                      try {
                        await resumeMerge.mutateAsync({});
                        toast.success(t(($) => $.releases.merge.resumed_toast));
                      } catch (err) {
                        toast.error(err instanceof Error ? err.message : String(err));
                      }
                    }}
                    disabled={resumeMerge.isPending}
                    data-testid="release-resume-button"
                  >
                    <Play className="size-3.5" />
                    {t(($) => $.releases.merge.resume_button)}
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => setResumeOpen(true)}
                    data-testid="release-resume-with-skip-button"
                  >
                    <SkipForward className="size-3.5" />
                    {t(($) => $.releases.merge.resume_with_skip_button)}
                  </Button>
                  <Button
                    size="sm"
                    variant="destructive"
                    onClick={() => setAbortOpen(true)}
                    data-testid="release-abort-button"
                  >
                    {t(($) => $.releases.merge.abort_button)}
                  </Button>
                </>
              )}
            </div>
          </header>

          {/* Paused banner. Renders the first failure's error so the
              user has immediate context before opening the resume
              dialog. */}
          {isPaused && (
            <div
              className="flex items-start gap-2 rounded border border-amber-500/40 bg-amber-500/10 p-3 text-sm text-amber-700 dark:text-amber-400"
              data-testid="release-paused-banner"
            >
              <Pause className="mt-0.5 size-4" />
              <div className="flex-1">
                <p className="font-medium">
                  {t(($) => $.releases.merge.paused_banner_title)}
                </p>
                {firstFailureError && (
                  <p className="text-xs">
                    {t(($) => $.releases.merge.paused_banner_description, {
                      error: firstFailureError,
                    })}
                  </p>
                )}
              </div>
            </div>
          )}

          {/* Start-merge preconditions (assembling only). Renders a
              soft-warning panel listing why the start button is
              disabled, so the user can resolve them. */}
          {isAssembling && startMergePreconditions.length > 0 && (
            <div
              className="rounded border bg-muted/40 p-3 text-xs"
              data-testid="release-start-preconditions"
            >
              <p className="mb-1 font-medium text-foreground">
                {t(($) => $.releases.merge.preconditions_failed_title)}
              </p>
              <ul className="list-disc pl-4 text-muted-foreground">
                {startMergePreconditions.map((reason) => (
                  <li key={reason}>{reason}</li>
                ))}
              </ul>
            </div>
          )}

          {release.description && (
            <p className="text-sm text-muted-foreground">{release.description}</p>
          )}

          {/* Stage progress bar. Highlight up to and including the
              current stage. Terminal-bad states (cancelled,
              rolled_back) render an inline alert instead of the bar. */}
          {isCancelled || isRolledBack ? (
            <div
              className="flex items-center gap-2 rounded border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive"
              data-testid="release-terminal-banner"
            >
              <AlertTriangle className="size-4" />
              {isCancelled
                ? t(($) => $.releases.event.cancelled)
                : t(($) => $.releases.stage.rolled_back)}
              {release.rollback_reason && (
                <span className="text-xs text-muted-foreground">
                  — {release.rollback_reason}
                </span>
              )}
            </div>
          ) : (
            <StageProgressBar
              currentStage={release.stage}
              merging={isMerging ? { merged: mergedCount, total: totalPRs } : null}
            />
          )}

          {/* Linked channel + issue chips. Each clickable to its own
              workspace-scoped page. */}
          <div className="flex flex-wrap items-center gap-3 text-sm">
            {data.channel && slug && (
              <AppLink
                href={`/${slug}/channels/${data.channel.id}`}
                className="inline-flex items-center gap-1.5 rounded bg-muted px-2 py-1 hover:bg-accent"
              >
                <MessagesSquare className="size-3.5" />
                <span>{t(($) => $.releases.detail.channel_link)}</span>
                <span className="text-muted-foreground">{`#${data.channel.name}`}</span>
              </AppLink>
            )}
            {data.issue && slug && (
              <AppLink
                href={`/${slug}/issues/${data.issue.identifier ?? data.issue.id}`}
                className="inline-flex items-center gap-1.5 rounded bg-muted px-2 py-1 hover:bg-accent"
              >
                <Hash className="size-3.5" />
                <span>{t(($) => $.releases.detail.issue_link)}</span>
                <span className="text-muted-foreground">{data.issue.title ?? ""}</span>
              </AppLink>
            )}
          </div>

          {/* PR list. */}
          <section data-testid="release-pr-list">
            <h2 className="mb-2 text-sm font-medium">
              {t(($) => $.releases.detail.prs_heading)}
            </h2>
            {data.pull_requests.length === 0 ? (
              <p className="text-sm text-muted-foreground">
                {t(($) => $.releases.detail.no_prs)}
              </p>
            ) : (
              <ul className="space-y-1">
                {data.pull_requests.map((pr) => (
                  <li
                    key={pr.id}
                    className="flex items-center gap-2 rounded border bg-card p-2 text-sm"
                  >
                    <a
                      href={pr.html_url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="flex flex-1 items-center gap-2 hover:underline"
                    >
                      <span className="tabular-nums text-muted-foreground">
                        #{pr.number}
                      </span>
                      <span className="truncate">{pr.title}</span>
                    </a>
                    <MergeStatePill pr={pr} />
                    {pr.merge_state === "failed" && isPaused && (
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={async () => {
                          try {
                            await resumeMerge.mutateAsync({});
                            toast.success(t(($) => $.releases.merge.resumed_toast));
                          } catch (err) {
                            toast.error(err instanceof Error ? err.message : String(err));
                          }
                        }}
                        data-testid="release-pr-retry-button"
                      >
                        {t(($) => $.releases.merge.retry_button)}
                      </Button>
                    )}
                    <span className="text-[10px] uppercase tracking-wide text-muted-foreground">
                      {pr.risk_level ?? "medium"}
                    </span>
                    {isAssembling && (
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={async () => {
                          try {
                            await removePR.mutateAsync(pr.id);
                          } catch (err) {
                            toast.error(
                              err instanceof Error ? err.message : String(err),
                            );
                          }
                        }}
                        aria-label={t(($) => $.releases.detail.remove_pr_tooltip)}
                        data-testid="release-pr-remove"
                      >
                        <X className="size-3" />
                      </Button>
                    )}
                  </li>
                ))}
              </ul>
            )}
          </section>

          {/* Event timeline. */}
          <section data-testid="release-event-timeline">
            <h2 className="mb-2 text-sm font-medium">
              {t(($) => $.releases.detail.events_heading)}
            </h2>
            {data.events.length === 0 ? (
              <p className="text-sm text-muted-foreground">
                {t(($) => $.releases.detail.no_events)}
              </p>
            ) : (
              <ul className="space-y-1.5">
                {data.events.map((event) => (
                  <li
                    key={event.id}
                    className="flex items-start gap-2 text-xs text-muted-foreground"
                  >
                    <span className="mt-0.5 size-1.5 shrink-0 rounded-full bg-primary" />
                    <span className="font-medium text-foreground">
                      {translateEventType(
                        // i18n.t is typed against the default namespace; the
                        // signature for `(key)` is rejected by the strict
                        // typed selector form, so we go through `as`.
                        (k) =>
                          (i18n as { t: (key: string, opts?: unknown) => string })
                            .t(k, { ns: "ship" }),
                        event.event_type,
                      )}
                    </span>
                    <span className="ml-auto tabular-nums">
                      {formatRelativeShort(event.created_at)}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </section>
        </div>
      </div>

      {/* Cancel confirmation. */}
      <AlertDialog open={cancelOpen} onOpenChange={setCancelOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.releases.detail.cancel_confirm_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.releases.detail.cancel_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="grid gap-1.5">
            <Label htmlFor="cancel-reason">
              {t(($) => $.releases.detail.cancel_reason_label)}
            </Label>
            <Textarea
              id="cancel-reason"
              value={cancelReason}
              onChange={(e) => setCancelReason(e.target.value)}
              rows={2}
            />
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>
              {t(($) => $.releases.create_dialog.cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={async () => {
                try {
                  await cancelMutation.mutateAsync({ reason: cancelReason });
                  toast.success(t(($) => $.releases.event.cancelled));
                  setCancelOpen(false);
                } catch (err) {
                  toast.error(err instanceof Error ? err.message : String(err));
                }
              }}
            >
              {t(($) => $.releases.detail.cancel_confirm_action)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Abort merge train confirmation. */}
      <AlertDialog open={abortOpen} onOpenChange={setAbortOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.releases.merge.abort_confirm_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.releases.merge.abort_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="grid gap-1.5">
            <Label htmlFor="abort-reason">
              {t(($) => $.releases.merge.abort_reason_label)}
            </Label>
            <Textarea
              id="abort-reason"
              value={abortReason}
              onChange={(e) => setAbortReason(e.target.value)}
              rows={2}
            />
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>
              {t(($) => $.releases.create_dialog.cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={async () => {
                try {
                  await abortMerge.mutateAsync({ reason: abortReason });
                  toast.success(t(($) => $.releases.merge.aborted_toast));
                  setAbortOpen(false);
                } catch (err) {
                  toast.error(err instanceof Error ? err.message : String(err));
                }
              }}
            >
              {t(($) => $.releases.merge.abort_confirm_action)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Resume merge train dialog (with optional skip list). */}
      <ResumeMergeDialog
        open={resumeOpen}
        onOpenChange={setResumeOpen}
        failedPRs={failedPRs}
        onSubmit={async (skipIds) => {
          try {
            await resumeMerge.mutateAsync({ skip_pr_ids: skipIds });
            toast.success(t(($) => $.releases.merge.resumed_toast));
            setResumeOpen(false);
          } catch (err) {
            toast.error(err instanceof Error ? err.message : String(err));
          }
        }}
        submitting={resumeMerge.isPending}
      />
    </div>
  );
}

interface ResumeMergeDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  failedPRs: ReleasePullRequest[];
  onSubmit: (skipIds: string[]) => Promise<void>;
  submitting: boolean;
}

function ResumeMergeDialog({
  open,
  onOpenChange,
  failedPRs,
  onSubmit,
  submitting,
}: ResumeMergeDialogProps) {
  const { t } = useT("ship");
  const [skipIds, setSkipIds] = useState<Set<string>>(new Set());

  const toggle = (id: string) => {
    setSkipIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {t(($) => $.releases.merge.resume_dialog_title)}
          </DialogTitle>
          <DialogDescription>
            {t(($) => $.releases.merge.resume_dialog_description)}
          </DialogDescription>
        </DialogHeader>
        <ul className="space-y-2" data-testid="resume-dialog-pr-list">
          {failedPRs.map((pr) => (
            <li key={pr.id} className="flex items-start gap-2 rounded border p-2 text-sm">
              <Checkbox
                id={`skip-${pr.id}`}
                checked={skipIds.has(pr.id)}
                onCheckedChange={() => toggle(pr.id)}
                data-testid="resume-dialog-skip-checkbox"
              />
              <div className="flex-1">
                <label
                  htmlFor={`skip-${pr.id}`}
                  className="cursor-pointer text-sm font-medium"
                >
                  #{pr.number} {pr.title}
                </label>
                {pr.merge_error && (
                  <p className="text-xs text-muted-foreground">{pr.merge_error}</p>
                )}
                <p className="mt-0.5 text-[11px] text-muted-foreground">
                  {t(($) => $.releases.merge.resume_dialog_skip_label)}
                </p>
              </div>
            </li>
          ))}
        </ul>
        <DialogFooter>
          <Button
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={submitting}
          >
            {t(($) => $.releases.create_dialog.cancel)}
          </Button>
          <Button
            onClick={() => onSubmit(Array.from(skipIds))}
            disabled={submitting}
            data-testid="resume-dialog-submit"
          >
            {t(($) => $.releases.merge.resume_dialog_submit)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function StageBadge({ stage }: { stage: string }) {
  const { t } = useT("ship");
  return (
    <span
      className={cn(
        "rounded px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide",
        stage === "done" && "bg-emerald-500/20 text-emerald-700 dark:text-emerald-400",
        stage === "rolled_back" && "bg-destructive/20 text-destructive",
        stage === "cancelled" && "bg-muted text-muted-foreground",
        !["done", "rolled_back", "cancelled"].includes(stage) &&
          "bg-primary/20 text-primary",
      )}
      data-testid="release-stage-badge"
    >
      {t(($) =>
        ($.releases.stage as Record<string, string>)[stage] ??
        $.releases.stage.assembling,
      )}
    </span>
  );
}

function RiskPill({ risk }: { risk: string }) {
  return (
    <span
      className={cn(
        "rounded px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide",
        risk === "critical" && "bg-destructive/20 text-destructive",
        risk === "high" && "bg-orange-500/20 text-orange-700 dark:text-orange-400",
        risk === "medium" && "bg-amber-500/20 text-amber-700 dark:text-amber-400",
        risk === "low" && "bg-muted text-muted-foreground",
      )}
    >
      {risk}
    </span>
  );
}

/** Per-PR merge_state pill. Renders one of the five pre-defined
 *  shapes; an unknown server-side value falls back to "queued"
 *  styling so a forward-compat backend roll never crashes the row. */
function MergeStatePill({ pr }: { pr: ReleasePullRequest }) {
  const { t } = useT("ship");
  const state = pr.merge_state;
  switch (state) {
    case "merging":
      return (
        <span
          className="inline-flex items-center gap-1 rounded bg-blue-500/20 px-1.5 py-0.5 text-[10px] font-medium text-blue-700 dark:text-blue-400"
          data-testid="release-pr-merge-state"
          data-state="merging"
        >
          <Loader2 className="size-3 animate-spin" />
          {t(($) => $.releases.merge_state.merging)}
        </span>
      );
    case "merged": {
      const shortSha = pr.merged_sha ? pr.merged_sha.slice(0, 7) : null;
      return (
        <span
          className="inline-flex items-center gap-1 rounded bg-emerald-500/20 px-1.5 py-0.5 text-[10px] font-medium text-emerald-700 dark:text-emerald-400"
          data-testid="release-pr-merge-state"
          data-state="merged"
        >
          <CheckCircle2 className="size-3" />
          {t(($) => $.releases.merge_state.merged)}
          {shortSha && <span className="font-mono">{shortSha}</span>}
        </span>
      );
    }
    case "failed":
      return (
        <span
          className="inline-flex items-center gap-1 rounded bg-destructive/20 px-1.5 py-0.5 text-[10px] font-medium text-destructive"
          data-testid="release-pr-merge-state"
          data-state="failed"
          title={pr.merge_error ?? undefined}
        >
          <XCircle className="size-3" />
          {t(($) => $.releases.merge_state.failed)}
        </span>
      );
    case "skipped":
      return (
        <span
          className="inline-flex items-center gap-1 rounded bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground"
          data-testid="release-pr-merge-state"
          data-state="skipped"
        >
          <SkipForward className="size-3" />
          {t(($) => $.releases.merge_state.skipped)}
        </span>
      );
    case "queued":
    default:
      return (
        <span
          className="inline-flex items-center gap-1 rounded bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground"
          data-testid="release-pr-merge-state"
          data-state="queued"
        >
          <CircleDashed className="size-3" />
          {t(($) => $.releases.merge_state.queued)}
        </span>
      );
  }
}

function StageProgressBar({
  currentStage,
  merging,
}: {
  currentStage: string;
  merging: { merged: number; total: number } | null;
}) {
  const { t } = useT("ship");
  const currentIdx = STAGE_PROGRESS.findIndex((s) => s.key === currentStage);
  return (
    <ol
      className="grid grid-cols-7 gap-1 text-[11px]"
      data-testid="release-stage-progress"
    >
      {STAGE_PROGRESS.map((s, i) => {
        const reached = currentIdx >= 0 && i <= currentIdx;
        const isMergingStep = s.key === "merging" && merging !== null;
        return (
          <li
            key={s.key}
            className={cn(
              "rounded px-1.5 py-1 text-center transition-colors",
              reached ? s.iconBg : "bg-muted/40 text-muted-foreground",
              i === currentIdx && "ring-1 ring-primary",
            )}
            data-state={i === currentIdx ? "current" : reached ? "reached" : "pending"}
          >
            {t(($) =>
              ($.releases.stage as Record<string, string>)[s.key] ??
              $.releases.stage.assembling,
            )}
            {/* While the merge train is active, render the per-PR
                progress fraction inline on the merging step. */}
            {isMergingStep && (
              <span className="ml-1 text-foreground tabular-nums">
                {merging.merged}/{merging.total}
              </span>
            )}
          </li>
        );
      })}
    </ol>
  );
}

/** Translate a release event_type to the user-facing label.
 *  We use the i18next instance directly (not a typed selector) so
 *  this helper stays decoupled from the namespace shape — the
 *  release log carries arbitrary event_type strings, including
 *  ones our locales don't know about (forward-compat per CLAUDE.md
 *  "Enum drift downgrades, not crashes"). */
function translateEventType(
  i18nT: (key: string) => string,
  eventType: string,
): string {
  const key = `releases.event.${eventType}`;
  const translated = i18nT(key);
  // i18next returns the key verbatim when missing — surface the
  // raw event_type as a defensive fallback in that case.
  return translated === key ? eventType : translated;
}

function formatRelativeShort(iso: string): string {
  if (!iso) return "";
  const then = new Date(iso).getTime();
  if (!Number.isFinite(then)) return "";
  const diff = Date.now() - then;
  const min = Math.floor(diff / 60_000);
  if (min < 1) return "just now";
  if (min < 60) return `${min}m`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h`;
  const day = Math.floor(hr / 24);
  return `${day}d`;
}

/** checkStartMergePreconditions returns a list of human-readable
 *  reasons the start_merge button should be disabled, or [] when
 *  it's safe to start. We intentionally surface these on the UI
 *  side too (the server enforces them at start_merge time) so the
 *  user sees why the button is greyed out before clicking. */
function checkStartMergePreconditions(
  data: NonNullable<ReturnType<typeof useReleaseDetail>["data"]>,
): string[] {
  const reasons: string[] = [];
  if (data.pull_requests.length === 0) {
    reasons.push("Add at least one PR");
  }
  for (const pr of data.pull_requests) {
    if (pr.state !== "open") {
      reasons.push(`PR #${pr.number} is ${pr.state}`);
    } else if (pr.is_draft) {
      reasons.push(`PR #${pr.number} is a draft`);
    } else if (pr.mergeable === "CONFLICTING") {
      reasons.push(`PR #${pr.number} has merge conflicts`);
    } else if (pr.ci_status && pr.ci_status !== "success") {
      reasons.push(`PR #${pr.number} CI is ${pr.ci_status}`);
    } else if (pr.review_decision && pr.review_decision !== "APPROVED") {
      reasons.push(`PR #${pr.number} review: ${pr.review_decision}`);
    }
  }
  const risk = data.release.risk_level;
  if ((risk === "medium" || risk === "high") && !data.release.approver_id) {
    reasons.push(`Risk ${risk} requires an approver`);
  }
  if (risk === "critical") {
    if (!data.release.approver_id) reasons.push("Critical risk requires an approver");
    if (!data.release.second_approver_id)
      reasons.push("Critical risk requires a second approver");
  }
  return reasons;
}
