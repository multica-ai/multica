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
  ExternalLink,
  FlaskConical,
  Hash,
  Loader2,
  MessagesSquare,
  Pause,
  Play,
  Rocket,
  ShieldCheck,
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
  useRunSmokeTestsForRelease,
  useMarkSmokeManualPass,
  useMarkReleaseVerified,
  useUnverifyRelease,
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
  // Phase 7c — staging-stage mutations.
  const runSmoke = useRunSmokeTestsForRelease(releaseId);
  const markSmokePass = useMarkSmokeManualPass(releaseId);
  const markVerified = useMarkReleaseVerified(releaseId);
  const unverify = useUnverifyRelease(releaseId);
  const [cancelOpen, setCancelOpen] = useState(false);
  const [cancelReason, setCancelReason] = useState("");
  const [abortOpen, setAbortOpen] = useState(false);
  const [abortReason, setAbortReason] = useState("");
  const [resumeOpen, setResumeOpen] = useState(false);
  // Phase 7c — staging-stage dialog state.
  const [verifyOpen, setVerifyOpen] = useState(false);
  const [verifyNote, setVerifyNote] = useState("");
  const [unverifyOpen, setUnverifyOpen] = useState(false);
  const [unverifyReason, setUnverifyReason] = useState("");
  const [smokePassOpen, setSmokePassOpen] = useState(false);
  const [smokePassNote, setSmokePassNote] = useState("");

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
  // Phase 7c — staging-stage flags. We split the in_staging stage
  // into "deploy not yet landed" vs. "deploy landed; smoke pending"
  // because they need different UI: the former renders a waiting
  // state, the latter the smoke + verify panels.
  const isInStaging = release.stage === "in_staging";
  const isVerifying = release.stage === "verifying";
  const isStagingOrVerifying = isInStaging || isVerifying;
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
              {isStagingOrVerifying && (
                <StagingActionButtons
                  release={release}
                  onRunSmoke={() => {
                    runSmoke
                      .mutateAsync(undefined)
                      .then(() =>
                        toast.success(t(($) => $.releases.staging.run_smoke_button)),
                      )
                      .catch((err: unknown) =>
                        toast.error(err instanceof Error ? err.message : String(err)),
                      );
                  }}
                  runSmokePending={runSmoke.isPending}
                  onMarkSmokePass={() => setSmokePassOpen(true)}
                  onMarkVerified={() => setVerifyOpen(true)}
                  onUnverify={() => setUnverifyOpen(true)}
                />
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

          {/* Phase 7c — staging-stage panels. Each is conditional on
              the relevant signal being present, so a release in
              earlier stages doesn't render any of them. */}
          {isStagingOrVerifying && <StagingPanels release={release} />}

          {/* Verified banner — only when the release reached
              verifying via mark_verified. */}
          {isVerifying && release.qa_verified_at && (
            <div
              className="flex items-start gap-2 rounded border border-emerald-500/40 bg-emerald-500/10 p-3 text-sm text-emerald-700 dark:text-emerald-400"
              data-testid="release-verified-banner"
            >
              <ShieldCheck className="mt-0.5 size-4" />
              <div className="flex-1">
                <p className="font-medium">
                  {t(($) => $.releases.verify.verified_banner, {
                    user: verifiedByLabel(release),
                  })}
                </p>
                <p className="text-xs text-muted-foreground">
                  {t(($) => $.releases.verify.verified_at, {
                    time: formatRelativeShort(release.qa_verified_at),
                  })}
                </p>
              </div>
            </div>
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

      {/* Phase 7c — Mark verified dialog. */}
      <Dialog open={verifyOpen} onOpenChange={setVerifyOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t(($) => $.releases.verify.dialog_title)}</DialogTitle>
            <DialogDescription>
              {t(($) => $.releases.verify.dialog_description)}
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-1.5">
            <Label htmlFor="verify-note">
              {t(($) => $.releases.verify.note_label)}
            </Label>
            <Textarea
              id="verify-note"
              value={verifyNote}
              onChange={(e) => setVerifyNote(e.target.value)}
              rows={2}
            />
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setVerifyOpen(false)}
              disabled={markVerified.isPending}
            >
              {t(($) => $.releases.create_dialog.cancel)}
            </Button>
            <Button
              onClick={async () => {
                try {
                  await markVerified.mutateAsync({ note: verifyNote });
                  toast.success(t(($) => $.releases.verify.submit));
                  setVerifyOpen(false);
                  setVerifyNote("");
                } catch (err) {
                  toast.error(err instanceof Error ? err.message : String(err));
                }
              }}
              disabled={markVerified.isPending}
              data-testid="release-verify-submit"
            >
              {markVerified.isPending
                ? t(($) => $.releases.verify.submitting)
                : t(($) => $.releases.verify.submit)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Phase 7c — Mark smoke pass dialog. */}
      <Dialog open={smokePassOpen} onOpenChange={setSmokePassOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {t(($) => $.releases.staging.manual_pass_dialog_title)}
            </DialogTitle>
            <DialogDescription>
              {t(($) => $.releases.staging.manual_pass_dialog_description)}
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-1.5">
            <Label htmlFor="smoke-pass-note">
              {t(($) => $.releases.staging.manual_pass_note_label)}
            </Label>
            <Textarea
              id="smoke-pass-note"
              value={smokePassNote}
              onChange={(e) => setSmokePassNote(e.target.value)}
              rows={2}
            />
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setSmokePassOpen(false)}
              disabled={markSmokePass.isPending}
            >
              {t(($) => $.releases.create_dialog.cancel)}
            </Button>
            <Button
              onClick={async () => {
                try {
                  await markSmokePass.mutateAsync({ note: smokePassNote });
                  toast.success(t(($) => $.releases.staging.manual_pass_submit));
                  setSmokePassOpen(false);
                  setSmokePassNote("");
                } catch (err) {
                  toast.error(err instanceof Error ? err.message : String(err));
                }
              }}
              disabled={markSmokePass.isPending}
              data-testid="release-smoke-pass-submit"
            >
              {t(($) => $.releases.staging.manual_pass_submit)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Phase 7c — Unverify destructive dialog. Reason is REQUIRED;
          the submit stays disabled until the user types something. */}
      <AlertDialog open={unverifyOpen} onOpenChange={setUnverifyOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.releases.unverify.dialog_title, { title: release.title })}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.releases.unverify.dialog_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="grid gap-1.5">
            <Label htmlFor="unverify-reason">
              {t(($) => $.releases.unverify.reason_label)}
            </Label>
            <Textarea
              id="unverify-reason"
              value={unverifyReason}
              onChange={(e) => setUnverifyReason(e.target.value)}
              rows={2}
              data-testid="release-unverify-reason"
            />
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>
              {t(($) => $.releases.create_dialog.cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              disabled={unverifyReason.trim() === "" || unverify.isPending}
              data-testid="release-unverify-submit"
              onClick={async (e) => {
                if (unverifyReason.trim() === "") {
                  e.preventDefault();
                  toast.error(t(($) => $.releases.unverify.reason_required));
                  return;
                }
                try {
                  await unverify.mutateAsync({ reason: unverifyReason });
                  toast.success(t(($) => $.releases.unverify.submit));
                  setUnverifyOpen(false);
                  setUnverifyReason("");
                } catch (err) {
                  toast.error(err instanceof Error ? err.message : String(err));
                }
              }}
            >
              {t(($) => $.releases.unverify.submit)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
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

// ---------------------------------------------------------------------------
// Phase 7c — staging-stage components.
// ---------------------------------------------------------------------------

/** StagingActionButtons renders the in_staging / verifying button row.
 *  We split this out of the header because the parent's button list
 *  was already heavy with merge-train branches; keeping the staging
 *  affordances colocated makes the gating logic easier to follow. */
function StagingActionButtons({
  release,
  onRunSmoke,
  runSmokePending,
  onMarkSmokePass,
  onMarkVerified,
  onUnverify,
}: {
  release: import("@multica/core/types").Release;
  onRunSmoke: () => void;
  runSmokePending: boolean;
  onMarkSmokePass: () => void;
  onMarkVerified: () => void;
  onUnverify: () => void;
}) {
  const { t } = useT("ship");
  const smoke = release.smoke_status ?? "";
  const isVerifying = release.stage === "verifying";
  const canRunSmoke = smoke !== "in_progress" && smoke !== "queued";
  const canManualPass =
    smoke === "completed_failure" || smoke === "skipped" || smoke === "";
  const smokePassedOrSkipped =
    smoke === "completed_success" || smoke === "manual_pass" || smoke === "skipped";

  if (isVerifying) {
    return (
      <Button
        size="sm"
        variant="destructive"
        onClick={onUnverify}
        data-testid="release-unverify-button"
      >
        {t(($) => $.releases.unverify.button)}
      </Button>
    );
  }

  return (
    <>
      {/* Run smoke is the primary affordance whenever it's not
          mid-flight; the "Re-run" copy applies after a completed run. */}
      <Button
        size="sm"
        variant="outline"
        onClick={onRunSmoke}
        disabled={!canRunSmoke || runSmokePending}
        data-testid="release-run-smoke-button"
      >
        <FlaskConical className="size-3.5" />
        {smoke === "completed_failure" || smoke === "completed_success"
          ? t(($) => $.releases.staging.rerun_smoke_button)
          : t(($) => $.releases.staging.run_smoke_button)}
      </Button>
      {canManualPass && (
        <Button
          size="sm"
          variant="outline"
          onClick={onMarkSmokePass}
          data-testid="release-manual-pass-button"
        >
          {t(($) => $.releases.staging.manual_pass_button)}
        </Button>
      )}
      <Button
        size="sm"
        onClick={onMarkVerified}
        disabled={!smokePassedOrSkipped && release.risk_level !== "low" && release.risk_level !== "medium"}
        data-testid="release-verify-button"
      >
        <ShieldCheck className="size-3.5" />
        {t(($) => $.releases.verify.button)}
      </Button>
    </>
  );
}

/** StagingPanels renders the Linked staging deploy + Smoke status
 *  panels side-by-side when the release is in_staging or verifying.
 *  Either panel renders an empty/awaiting state when its underlying
 *  data isn't populated yet. */
function StagingPanels({
  release,
}: {
  release: import("@multica/core/types").Release;
}) {
  const { t } = useT("ship");
  const smoke = release.smoke_status ?? "";
  return (
    <div className="grid gap-3 sm:grid-cols-2" data-testid="release-staging-panels">
      {/* Deploy panel. */}
      <div className="rounded border bg-card p-3 text-sm">
        <div className="mb-1 flex items-center gap-1.5 text-xs font-medium uppercase tracking-wide text-muted-foreground">
          <Rocket className="size-3.5" />
          {t(($) => $.releases.staging.deploy_panel_title)}
        </div>
        {release.staging_deploy_id ? (
          <div className="space-y-1">
            {release.merged_main_sha && (
              <p className="font-mono text-xs">
                {release.merged_main_sha.slice(0, 7)}
              </p>
            )}
            {release.staged_at && (
              <p className="text-xs text-muted-foreground">
                {t(($) => $.releases.staging.deploy_at)}:{" "}
                {formatRelativeShort(release.staged_at)}
              </p>
            )}
          </div>
        ) : (
          <p className="text-xs text-muted-foreground">
            {t(($) => $.releases.staging.awaiting_deploy)}
          </p>
        )}
      </div>

      {/* Smoke panel. */}
      <div
        className="rounded border bg-card p-3 text-sm"
        data-testid="release-smoke-panel"
        data-smoke-status={smoke}
      >
        <div className="mb-1 flex items-center gap-1.5 text-xs font-medium uppercase tracking-wide text-muted-foreground">
          <FlaskConical className="size-3.5" />
          {t(($) => $.releases.staging.smoke_panel_title)}
        </div>
        <SmokeStatusPill status={smoke} />
        {release.smoke_run_url && (
          <a
            href={release.smoke_run_url}
            target="_blank"
            rel="noopener noreferrer"
            className="mt-1 inline-flex items-center gap-1 text-xs text-primary hover:underline"
          >
            <ExternalLink className="size-3" />
            {t(($) => $.releases.staging.view_run_link)}
          </a>
        )}
      </div>
    </div>
  );
}

/** SmokeStatusPill — known-status switch with a generic fallback for
 *  forward-compat. New backend smoke_status values render the raw
 *  string rather than crashing. */
function SmokeStatusPill({ status }: { status: string }) {
  const { t } = useT("ship");
  switch (status) {
    case "queued":
      return (
        <span className="inline-flex items-center gap-1 rounded bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
          <CircleDashed className="size-3" />
          {t(($) => $.releases.staging.smoke_status_queued)}
        </span>
      );
    case "in_progress":
      return (
        <span className="inline-flex items-center gap-1 rounded bg-blue-500/20 px-1.5 py-0.5 text-[10px] font-medium text-blue-700 dark:text-blue-400">
          <Loader2 className="size-3 animate-spin" />
          {t(($) => $.releases.staging.smoke_status_in_progress)}
        </span>
      );
    case "completed_success":
      return (
        <span className="inline-flex items-center gap-1 rounded bg-emerald-500/20 px-1.5 py-0.5 text-[10px] font-medium text-emerald-700 dark:text-emerald-400">
          <CheckCircle2 className="size-3" />
          {t(($) => $.releases.staging.smoke_status_completed_success)}
        </span>
      );
    case "completed_failure":
      return (
        <span className="inline-flex items-center gap-1 rounded bg-destructive/20 px-1.5 py-0.5 text-[10px] font-medium text-destructive">
          <XCircle className="size-3" />
          {t(($) => $.releases.staging.smoke_status_completed_failure)}
        </span>
      );
    case "skipped":
      return (
        <span className="inline-flex items-center gap-1 rounded bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
          <SkipForward className="size-3" />
          {t(($) => $.releases.staging.smoke_status_skipped)}
        </span>
      );
    case "manual_pass":
      return (
        <span className="inline-flex items-center gap-1 rounded bg-purple-500/20 px-1.5 py-0.5 text-[10px] font-medium text-purple-700 dark:text-purple-400">
          <ShieldCheck className="size-3" />
          {t(($) => $.releases.staging.smoke_status_manual_pass)}
        </span>
      );
    default:
      return (
        <span className="inline-flex items-center gap-1 rounded bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
          {status || "—"}
        </span>
      );
  }
}

/** verifiedByLabel produces a short identifier for the Verified
 *  banner. The release row only carries qa_verified_by as a UUID;
 *  we render an 8-char slice so the banner is meaningful without
 *  requiring the caller to fetch the user's display name. The full
 *  identity becomes available when we link out to the audit log. */
function verifiedByLabel(
  release: import("@multica/core/types").Release,
): string {
  if (release.qa_verified_by) {
    return release.qa_verified_by.slice(0, 8);
  }
  return "—";
}
