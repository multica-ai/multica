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

import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  CircleDashed,
  CornerDownRight,
  ExternalLink,
  FlaskConical,
  Hash,
  Heart,
  Loader2,
  MessagesSquare,
  Pause,
  Play,
  RefreshCw,
  Rocket,
  RotateCcw,
  ShieldCheck,
  Train,
  TrendingUp,
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
  useMarkReleaseStagingDeployed,
  usePromoteRelease,
  useMarkReleaseProductionDeployed,
  useRollbackRelease,
  useMarkReleaseDone,
  useOpenReleaseChannel,
  useReleaseHealth,
  useDeployEnvironments,
} from "@multica/core/ship";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { projectListOptions } from "@multica/core/projects/queries";
import { shipKeys } from "@multica/core/ship";
import type { ReleasePullRequest } from "@multica/core/types";
import { useQuery, useQueryClient } from "@tanstack/react-query";
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
import { AppLink, useNavigation } from "../../navigation";

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

// Staging-only stages — hidden from the progress bar when the project
// has no `kind='staging'` deploy environment. The backend skips these
// stages in the flow (see completeMergeTrain in release_merge.go) so
// they're never reachable for those projects; rendering them as
// permanently-pending chips just clutters the bar and confuses users.
const STAGING_ONLY_STAGES = new Set(["in_staging", "verifying"]);

/**
 * Map a backend mutation error to user-friendly text.
 *
 * The Ship Hub backend rejects out-of-order stage transitions with
 * messages like:
 *
 *   release: stage does not allow this transition: stage=in_production, want verifying
 *
 * Those leak two backend invariants (the stage names + the wire format
 * of the precondition check) into a user-facing toast, which is at
 * best confusing and at worst alarming for someone whose only "crime"
 * was clicking Promote on a dialog they opened a second too late. The
 * usual cause is a stale UI: the page hasn't yet caught up to a
 * background auto-advance.
 *
 * This helper recognizes the stage-mismatch pattern and returns a
 * friendlier message. Everything else falls through to the raw error
 * string so we don't accidentally swallow real problems.
 */
function friendlyMutationError(err: unknown): string {
  const raw = friendlyMutationError(err);
  // Pattern: "stage does not allow this transition: stage=<from>, want <to>"
  const m = raw.match(
    /stage does not allow this transition: stage=([\w_]+), want ([\w_]+)/,
  );
  if (m) {
    const [, from] = m;
    if (from === "in_production" || from === "done") {
      return "This release is already live in production — refresh the page to see the latest state.";
    }
    if (from === "cancelled" || from === "rolled_back") {
      return "This release has been closed (cancelled or rolled back) and can't be advanced further.";
    }
    // Generic: a stale UI tried to apply an action that the backend
    // can no longer accept from the current stage.
    return "This action is no longer valid for the release's current stage — refresh to see the latest state.";
  }
  return raw;
}

interface ShipReleasePageProps {
  releaseId: string;
}

export function ShipReleasePage({ releaseId }: ShipReleasePageProps) {
  const { t, i18n } = useT("ship");
  const workspace = useCurrentWorkspace();
  // Read wsId from the workspace store (same source as `workspace` above)
  // rather than the React Context — tests mock the store directly and
  // don't wrap WorkspaceIdProvider, so a Context-based read crashes the
  // test mount.
  const wsId = workspace?.id ?? "";
  const wsPaths = useWorkspacePaths();
  const queryClient = useQueryClient();
  const { data, isLoading, isError, isFetching } = useReleaseDetail(
    releaseId,
    true,
  );
  // Used for the project chip in the header. Hits the projects cache,
  // so no extra round-trip when the user lands here from the Ship Hub
  // page — that page already populated this query.
  const projectsQuery = useQuery(projectListOptions(wsId));
  const project = useMemo(() => {
    const pid = data?.release.project_id;
    if (!pid) return null;
    return (projectsQuery.data ?? []).find((p) => p.id === pid) ?? null;
  }, [projectsQuery.data, data?.release.project_id]);
  // Project's deploy envs drive the "show staging stages?" decision in
  // the progress bar. The backend skips in_staging/verifying entirely
  // when no staging env exists, so rendering those chips as forever-
  // pending is just noise. The hook reads from the projects cache and
  // is cheap (workspace-scoped, refetch on focus only).
  //
  // While the envs query is loading, default to TRUE so we don't
  // briefly hide chips that should be visible and then flicker them
  // back in.
  const projectId = data?.release.project_id ?? "";
  const deployEnvsQuery = useDeployEnvironments(projectId);
  const hasStaging = useMemo(() => {
    if (!projectId) return true;
    if (deployEnvsQuery.isLoading) return true;
    const envs = deployEnvsQuery.data?.environments ?? [];
    return envs.some((env) => env.kind === "staging");
  }, [projectId, deployEnvsQuery.isLoading, deployEnvsQuery.data]);
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
  const markStaged = useMarkReleaseStagingDeployed(releaseId);
  // Phase 7d — production-stage mutations + health rollup query.
  const promote = usePromoteRelease(releaseId);
  const markProdDeployed = useMarkReleaseProductionDeployed(releaseId);
  const rollback = useRollbackRelease(releaseId);
  const markDone = useMarkReleaseDone(releaseId);
  // Health query is enabled when the release reaches in_production
  // (the rollup row is empty until then; we still let the panel render
  // the "monitoring will begin" empty state for verifying / promoting).
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
  // Phase 7d — production dialog state.
  const [promoteOpen, setPromoteOpen] = useState(false);
  const [promoteRollbackPlan, setPromoteRollbackPlan] = useState("");
  const [promoteAck, setPromoteAck] = useState(false);
  const [rollbackOpen, setRollbackOpen] = useState(false);
  const [rollbackReason, setRollbackReason] = useState("");
  // Mark-production-deployed is debounced behind a 30s waiting state
  // so users don't click before a webhook lands. Tracks "stage entered
  // promoting at" — once 30s have elapsed since that, the affordance
  // appears.
  const [promotingSince, setPromotingSince] = useState<number | null>(null);
  const [showProductionEscapeHatch, setShowProductionEscapeHatch] = useState(false);

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

  // Stage flags. We compute these from `data?.release.stage` so they
  // can be used by hooks BEFORE the loading/isError early returns.
  // Rules-of-hooks: every hook in this component must run on every
  // render in the same order — pulling these out of the post-return
  // body and placing them here is what keeps useReleaseHealth +
  // the effect below at a stable position in the call list.
  const stage = data?.release.stage ?? "assembling";
  const isPromotingStage = stage === "promoting";
  const isHealthEnabled =
    stage === "in_production" || stage === "rolled_back" || stage === "done";

  // Health rollup is fetched once the release reaches in_production
  // (the monitor only writes rows for that stage); for earlier stages
  // we still let the panel render an empty state via the disabled
  // query (it returns an empty default).
  const releaseHealth = useReleaseHealth(releaseId, isHealthEnabled);

  // Phase 7d UX: the "Mark production deployed" escape hatch is
  // hidden for the first 30s after entering the promoting stage so
  // the user doesn't click before the deploy webhook has had a chance
  // to land. Anchored to release.promoted_at (server-side timestamp)
  // rather than page-mount time — otherwise navigating away and back
  // would restart the wait, even if the release entered promoting
  // hours ago.
  const promotedAt = data?.release.promoted_at ?? null;
  useEffect(() => {
    if (!isPromotingStage) {
      setPromotingSince(null);
      setShowProductionEscapeHatch(false);
      return undefined;
    }
    const promotedMs = promotedAt ? new Date(promotedAt).getTime() : Date.now();
    setPromotingSince(promotedMs);
    const elapsed = Date.now() - promotedMs;
    const DEBOUNCE_MS = 30_000;
    if (elapsed >= DEBOUNCE_MS) {
      // Already past the wait — show immediately.
      setShowProductionEscapeHatch(true);
      return undefined;
    }
    setShowProductionEscapeHatch(false);
    const timer = window.setTimeout(() => {
      setShowProductionEscapeHatch(true);
    }, DEBOUNCE_MS - elapsed);
    return () => window.clearTimeout(timer);
  }, [isPromotingStage, promotedAt]);

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
  // Phase 7d — production-stage flags.
  const isPromoting = release.stage === "promoting";
  const isInProduction = release.stage === "in_production";
  const isDone = release.stage === "done";
  const slug = workspace?.slug ?? "";
  // promotingSince is read by the effect above; suppress the
  // unused-binding warning here since the state setter is what the
  // 30s debounce relies on for re-render gating.
  void promotingSince;

  // Counts for the merge progress badge. Computed off the PR rows
  // because the release row only carries pr_count (total).
  const totalPRs = data.pull_requests.length;
  const mergedCount = data.pull_requests.filter((pr) => pr.merge_state === "merged").length;
  const inFlight = data.pull_requests.find((pr) => pr.merge_state === "merging");

  const startMergePreconditions = checkStartMergePreconditions(data, workspace);
  const canStartMerge = isAssembling && startMergePreconditions.length === 0;

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="px-5">
        <Train className="size-4 text-primary" />
        {project && (
          <>
            <AppLink
              href={wsPaths.projectDetail(project.id)}
              className="ml-2 inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
              data-testid="release-header-project-chip"
            >
              <span className="truncate max-w-[12rem]">{project.title}</span>
            </AppLink>
            <span className="text-xs text-muted-foreground/60">/</span>
          </>
        )}
        <h1
          className={cn(
            "text-sm font-medium",
            !project && "ml-2",
          )}
        >
          {release.title}
        </h1>
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
              {/* Always-on refresh. Backstops the WS / auto-poll for the
                  case where the tab was backgrounded long enough for
                  the WS hub to disconnect. The spinner state reflects
                  any in-flight refetch — automatic OR manual. */}
              <Button
                size="sm"
                variant="ghost"
                onClick={() =>
                  queryClient.invalidateQueries({
                    queryKey: shipKeys.releaseDetail(wsId, releaseId),
                  })
                }
                disabled={isFetching}
                title={t(($) => $.releases.detail.refresh_tooltip)}
                aria-label={t(($) => $.releases.detail.refresh_aria)}
                data-testid="release-refresh-button"
              >
                <RefreshCw
                  className={cn(
                    "size-3.5",
                    isFetching && "animate-spin",
                  )}
                />
              </Button>
              {isAssembling && (
                <>
                  <Button
                    size="sm"
                    onClick={async () => {
                      try {
                        await startMerge.mutateAsync({});
                        toast.success(t(($) => $.releases.merge.started_toast));
                      } catch (err) {
                        toast.error(friendlyMutationError(err));
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
                        toast.error(friendlyMutationError(err));
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
                  smokeWorkflowConfigured={
                    workspace?.ship_hub_smoke_workflow_set === true
                  }
                  onRunSmoke={() => {
                    runSmoke
                      .mutateAsync(undefined)
                      .then(() =>
                        toast.success(t(($) => $.releases.staging.run_smoke_button)),
                      )
                      .catch((err: unknown) =>
                        toast.error(friendlyMutationError(err)),
                      );
                  }}
                  runSmokePending={runSmoke.isPending}
                  onMarkSmokePass={() => setSmokePassOpen(true)}
                  onMarkVerified={() => setVerifyOpen(true)}
                  onUnverify={() => setUnverifyOpen(true)}
                  onMarkStaged={() => {
                    markStaged
                      .mutateAsync()
                      .then(() =>
                        toast.success(
                          t(($) => $.releases.staging.mark_staged_toast),
                        ),
                      )
                      .catch((err: unknown) =>
                        toast.error(
                          friendlyMutationError(err),
                        ),
                      );
                  }}
                  markStagedPending={markStaged.isPending}
                />
              )}
              {/* Phase 7d — Promote button on verifying stage. */}
              {isVerifying && (
                <Button
                  size="sm"
                  onClick={() => setPromoteOpen(true)}
                  data-testid="release-promote-button"
                >
                  <Rocket className="size-3.5" />
                  {t(($) => $.releases.promote.button)}
                </Button>
              )}
              {/* Phase 7d — promoting stage actions. Show progress + the
                  manual escape-hatch (after 30s) + a destructive
                  Cancel-and-rollback. */}
              {isPromoting && (
                <>
                  <span
                    className="inline-flex items-center gap-1.5 text-xs text-muted-foreground"
                    data-testid="release-promoting-progress"
                  >
                    <Loader2 className="size-3 animate-spin" />
                    {t(($) => $.releases.production.promoting_progress)}
                  </span>
                  {showProductionEscapeHatch && (
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => {
                        markProdDeployed
                          .mutateAsync()
                          .then(() =>
                            toast.success(
                              t(($) => $.releases.production.mark_deployed_toast),
                            ),
                          )
                          .catch((err: unknown) =>
                            toast.error(
                              friendlyMutationError(err),
                            ),
                          );
                      }}
                      disabled={markProdDeployed.isPending}
                      data-testid="release-mark-prod-deployed-button"
                    >
                      <Rocket className="size-3.5" />
                      {t(($) => $.releases.production.mark_deployed_button)}
                    </Button>
                  )}
                  <Button
                    size="sm"
                    variant="destructive"
                    onClick={() => setRollbackOpen(true)}
                    data-testid="release-rollback-button"
                  >
                    <RotateCcw className="size-3.5" />
                    {t(($) => $.releases.rollback.button)}
                  </Button>
                </>
              )}
              {/* Phase 7d — in_production actions: Rollback + Mark done. */}
              {isInProduction && (
                <>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => {
                      markDone
                        .mutateAsync()
                        .then(() =>
                          toast.success(t(($) => $.releases.production.mark_done_toast)),
                        )
                        .catch((err: unknown) =>
                          toast.error(friendlyMutationError(err)),
                        );
                    }}
                    disabled={markDone.isPending}
                    data-testid="release-mark-done-button"
                  >
                    <CheckCircle2 className="size-3.5" />
                    {t(($) => $.releases.production.mark_done_button)}
                  </Button>
                  <Button
                    size="sm"
                    variant="destructive"
                    onClick={() => setRollbackOpen(true)}
                    data-testid="release-rollback-button"
                  >
                    <RotateCcw className="size-3.5" />
                    {t(($) => $.releases.rollback.button)}
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
              hasStaging={hasStaging}
            />
          )}

          {/* Phase 7c polish — "Next step" banner. The release page used to
              leave users staring at the stage badge after the merge train
              completed without telling them what's expected to happen
              next or what action they could take. The banner names the
              gating condition + the affordance, so an inert release
              doesn't look like a halted one. */}
          {(isStagingOrVerifying || isPromoting || isInProduction || isDone) && (
            <NextStepBanner
              release={release}
              stagingPollerOn={
                workspace?.ship_hub_deploy_workflow_staging_set === true
              }
              productionPollerOn={
                workspace?.ship_hub_deploy_workflow_production_set === true
              }
            />
          )}

          {/* Phase 7d — production live banner. Appears once the
              release reaches in_production; renders alongside the
              health panel below. */}
          {isInProduction && release.promoted_at && (
            <div
              className="flex items-start gap-2 rounded border border-emerald-500/40 bg-emerald-500/10 p-3 text-sm text-emerald-700 dark:text-emerald-400"
              data-testid="release-live-banner"
            >
              <Rocket className="mt-0.5 size-4" />
              <p className="flex-1 font-medium">
                {t(($) => $.releases.production.live_banner, {
                  time: formatRelativeShort(release.promoted_at ?? ""),
                })}
              </p>
            </div>
          )}

          {/* Phase 7d — rolled-back banner. Read-only. */}
          {isRolledBack && (
            <div
              className="flex items-start gap-2 rounded border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive"
              data-testid="release-rolled-back-banner"
            >
              <RotateCcw className="mt-0.5 size-4" />
              <div className="flex-1">
                <p className="font-medium">
                  {t(($) => $.releases.rollback.rolled_back_banner, {
                    time: formatRelativeShort(release.rolled_back_completed_at ?? ""),
                    user: rolledBackByLabel(release),
                  })}
                </p>
                {release.rollback_reason && (
                  <p className="text-xs text-muted-foreground">
                    {t(($) => $.releases.rollback.rolled_back_reason, {
                      reason: release.rollback_reason ?? "",
                    })}
                  </p>
                )}
              </div>
            </div>
          )}

          {/* Phase 7d — production deploy panel + health rollup. Only
              renders for the production-stage flows. */}
          {(isPromoting || isInProduction || isRolledBack || isDone) && (
            <ProductionPanels
              release={release}
              health={releaseHealth.data ?? null}
              repoUrl={data.pull_requests[0]?.repo_url}
              productionPollerOn={
                workspace?.ship_hub_deploy_workflow_production_set === true
              }
            />
          )}

          {/* Phase 7c — staging-stage panels. Each is conditional on
              the relevant signal being present, so a release in
              earlier stages doesn't render any of them. */}
          {isStagingOrVerifying && (
            <StagingPanels
              release={release}
              repoUrl={data.pull_requests[0]?.repo_url}
              smokeWorkflowConfigured={
                workspace?.ship_hub_smoke_workflow_set === true
              }
              stagingPollerOn={
                workspace?.ship_hub_deploy_workflow_staging_set === true
              }
            />
          )}

          {/* Phase 7d follow-up — two-approver "awaiting second
              signoff" banner. Renders when the release is using the
              "two" rule and exactly one slot has signed off. The
              pending approver gets the active Mark-verified button
              from StagingActionButtons; everyone else's button is
              gated by canVerifyRelease at the server. */}
          {data.signoffs && data.signoffs.length === 1 && release.stage === "in_staging" && (
            <div
              className="flex items-start gap-2 rounded border border-amber-500/40 bg-amber-500/10 p-3 text-sm text-amber-700 dark:text-amber-400"
              data-testid="release-pending-second-approver-banner"
            >
              <ShieldCheck className="mt-0.5 size-4" />
              <div className="flex-1">
                <p className="font-medium">
                  {data.signoffs[0]?.approver_slot === "first"
                    ? t(($) => $.releases.approval_rule.rule_two_second_required, {
                        name: pendingSecondApproverName(release, data.signoffs ?? []),
                      })
                    : t(($) => $.releases.approval_rule.rule_two_first_required, {
                        name: pendingSecondApproverName(release, data.signoffs ?? []),
                      })}
                </p>
                <p className="text-xs text-muted-foreground">
                  {t(($) => $.releases.approval_rule.rule_two_signoff_recorded)}
                </p>
              </div>
            </div>
          )}

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
              workspace-scoped page. The channel slot also doubles as
              the "Open discussion channel" affordance — channels are
              now opt-in (no longer auto-created on release create), so
              we render the chip when one exists and a button when one
              doesn't. The release issue is always present (still
              auto-created — the canonical tracked record).  */}
          <div className="flex flex-wrap items-center gap-3 text-sm">
            {data.channel && slug ? (
              <AppLink
                href={`/${slug}/channels/${data.channel.id}`}
                className="inline-flex items-center gap-1.5 rounded bg-muted px-2 py-1 hover:bg-accent"
                data-testid="release-channel-link"
              >
                <MessagesSquare className="size-3.5" />
                <span>{t(($) => $.releases.detail.channel_link)}</span>
                <span className="text-muted-foreground">{`#${data.channel.name}`}</span>
              </AppLink>
            ) : (
              <OpenReleaseChannelButton releaseId={data.release.id} />
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
                      className="group/prlink flex flex-1 items-center gap-2 hover:underline"
                      data-testid="release-pr-row-link"
                    >
                      <span className="tabular-nums text-muted-foreground">
                        #{pr.number}
                      </span>
                      <span className="truncate">{pr.title}</span>
                      <ExternalLink
                        className="size-3 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover/prlink:opacity-100"
                        aria-hidden
                      />
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
                            toast.error(friendlyMutationError(err));
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
                              friendlyMutationError(err),
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
                {data.events.map((event) => {
                  const label = translateEventType(
                    (k) =>
                      (i18n as { t: (key: string, opts?: unknown) => string })
                        .t(k, { ns: "ship" }),
                    event.event_type,
                  );
                  // Phase 7c polish — render a deep link when the event
                  // payload carries a navigable id. Channel/issue events
                  // are the most-clicked "where do I go next?" pivots
                  // from the timeline; PR-add/remove + paused-PR get
                  // links to the GitHub PR via the cached pull_requests
                  // list. Falls through to plain text on any miss.
                  const link = resolveEventLink(event, data, workspace?.slug ?? "");
                  return (
                    <li
                      key={event.id}
                      className="flex items-start gap-2 text-xs text-muted-foreground"
                    >
                      <span className="mt-0.5 size-1.5 shrink-0 rounded-full bg-primary" />
                      {link ? (
                        link.external ? (
                          <a
                            href={link.href}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="font-medium text-foreground hover:underline"
                            data-testid="release-event-link"
                          >
                            {label}
                          </a>
                        ) : (
                          <AppLink
                            href={link.href}
                            className="font-medium text-foreground hover:underline"
                            data-testid="release-event-link"
                          >
                            {label}
                          </AppLink>
                        )
                      ) : (
                        <span className="font-medium text-foreground">{label}</span>
                      )}
                      <time
                        dateTime={event.created_at}
                        title={`${new Date(event.created_at).toLocaleString()} (${formatRelativeShort(event.created_at)} ago)`}
                        className="ml-auto whitespace-nowrap tabular-nums"
                      >
                        {formatAbsoluteShort(event.created_at)}
                      </time>
                    </li>
                  );
                })}
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
                  toast.error(friendlyMutationError(err));
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
                  toast.error(friendlyMutationError(err));
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
            toast.error(friendlyMutationError(err));
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
                  toast.error(friendlyMutationError(err));
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
                  toast.error(friendlyMutationError(err));
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
                  toast.error(friendlyMutationError(err));
                }
              }}
            >
              {t(($) => $.releases.unverify.submit)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Phase 7d — Promote dialog. Captures the rollback plan and the
          deploy-intent acknowledgement before submitting. The submit
          stays disabled when the rollback plan is required (high /
          critical risk) but missing, or the ack hasn't been checked. */}
      <Dialog open={promoteOpen} onOpenChange={setPromoteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t(($) => $.releases.promote.dialog_title)}</DialogTitle>
            <DialogDescription>
              {t(($) => $.releases.promote.dialog_description)}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            <div className="text-xs">
              <span className="text-muted-foreground">
                {t(($) => $.releases.promote.summary_label)}:{" "}
              </span>
              <span className="text-foreground">
                {t(($) => $.releases.promote.summary_prs, {
                  count: data.pull_requests.length,
                })}
              </span>
              {release.merged_main_sha && (
                <>
                  {" · "}
                  <span className="text-muted-foreground">
                    {t(($) => $.releases.promote.summary_sha)}:{" "}
                  </span>
                  <CommitLink
                    repoUrl={data.pull_requests[0]?.repo_url}
                    sha={release.merged_main_sha}
                    className="text-foreground"
                  />
                </>
              )}
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="promote-rollback-plan">
                {t(($) => $.releases.promote.rollback_plan_label)}
              </Label>
              <Textarea
                id="promote-rollback-plan"
                value={promoteRollbackPlan}
                onChange={(e) => setPromoteRollbackPlan(e.target.value)}
                placeholder={t(($) => $.releases.promote.rollback_plan_placeholder)}
                rows={3}
                data-testid="release-promote-rollback-plan"
              />
            </div>
            <label className="flex items-center gap-2 text-sm">
              <Checkbox
                checked={promoteAck}
                onCheckedChange={(c) => setPromoteAck(c === true)}
                data-testid="release-promote-ack"
              />
              <span>{t(($) => $.releases.promote.ack_label)}</span>
            </label>
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setPromoteOpen(false)}
              disabled={promote.isPending}
            >
              {t(($) => $.releases.create_dialog.cancel)}
            </Button>
            <Button
              disabled={
                promote.isPending ||
                !promoteAck ||
                ((release.risk_level === "high" || release.risk_level === "critical") &&
                  promoteRollbackPlan.trim() === "")
              }
              data-testid="release-promote-submit"
              onClick={async () => {
                if (!promoteAck) {
                  toast.error(t(($) => $.releases.promote.ack_required));
                  return;
                }
                if (
                  (release.risk_level === "high" || release.risk_level === "critical") &&
                  promoteRollbackPlan.trim() === ""
                ) {
                  toast.error(t(($) => $.releases.promote.rollback_plan_required));
                  return;
                }
                try {
                  await promote.mutateAsync({
                    rollback_plan: promoteRollbackPlan.trim() || undefined,
                  });
                  toast.success(t(($) => $.releases.promote.promoted_toast));
                  setPromoteOpen(false);
                  setPromoteRollbackPlan("");
                  setPromoteAck(false);
                } catch (err) {
                  toast.error(friendlyMutationError(err));
                }
              }}
            >
              {promote.isPending
                ? t(($) => $.releases.promote.submitting)
                : t(($) => $.releases.promote.submit)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Phase 7d — Rollback dialog. Reason REQUIRED. Lists the merged
          PRs in reverse-merge order with deep links so the user can
          click GitHub's per-PR Revert button. */}
      <RollbackDialog
        open={rollbackOpen}
        onOpenChange={setRollbackOpen}
        mergedPRs={data.pull_requests.filter((pr) => pr.merge_state === "merged")}
        reason={rollbackReason}
        onReasonChange={setRollbackReason}
        onSubmit={async () => {
          if (rollbackReason.trim() === "") {
            toast.error(t(($) => $.releases.rollback.reason_required));
            return;
          }
          try {
            await rollback.mutateAsync({ reason: rollbackReason.trim() });
            toast.success(t(($) => $.releases.rollback.submit));
            setRollbackOpen(false);
            setRollbackReason("");
          } catch (err) {
            toast.error(friendlyMutationError(err));
          }
        }}
        submitting={rollback.isPending}
      />
    </div>
  );
}

// ---------------------------------------------------------------------------
// Phase 7d — production-stage components.
// ---------------------------------------------------------------------------

interface RollbackDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  mergedPRs: ReleasePullRequest[];
  reason: string;
  onReasonChange: (v: string) => void;
  onSubmit: () => Promise<void>;
  submitting: boolean;
}

function RollbackDialog({
  open,
  onOpenChange,
  mergedPRs,
  reason,
  onReasonChange,
  onSubmit,
  submitting,
}: RollbackDialogProps) {
  const { t } = useT("ship");
  // Reverse merge order — last merged appears first. The mergedPRs
  // input is already filtered to merge_state="merged"; we just sort
  // by position descending to mirror the backend's
  // ListReleasePRsByMergeOrderDesc.
  const ordered = useMemo(
    () => [...mergedPRs].sort((a, b) => b.position - a.position),
    [mergedPRs],
  );
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            {t(($) => $.releases.rollback.dialog_title)}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t(($) => $.releases.rollback.dialog_description)}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className="grid gap-1.5">
          <Label htmlFor="rollback-reason">
            {t(($) => $.releases.rollback.reason_label)}
          </Label>
          <Textarea
            id="rollback-reason"
            value={reason}
            onChange={(e) => onReasonChange(e.target.value)}
            placeholder={t(($) => $.releases.rollback.reason_placeholder)}
            rows={2}
            data-testid="release-rollback-reason"
          />
        </div>
        {ordered.length > 0 && (
          <div className="space-y-1.5">
            <p className="text-xs font-medium text-foreground">
              {t(($) => $.releases.rollback.merged_prs_heading)}
            </p>
            <ul
              className="max-h-48 space-y-1 overflow-y-auto text-xs"
              data-testid="release-rollback-pr-list"
            >
              {ordered.map((pr) => (
                <li key={pr.id} className="flex items-center gap-2">
                  <span className="tabular-nums text-muted-foreground">
                    #{pr.number}
                  </span>
                  <a
                    href={pr.html_url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="flex-1 truncate hover:underline"
                    title={t(($) => $.releases.rollback.github_revert_hint)}
                  >
                    {pr.title}
                  </a>
                  <ExternalLink className="size-3 text-muted-foreground" aria-hidden />
                </li>
              ))}
            </ul>
            <p className="text-[11px] text-muted-foreground">
              {t(($) => $.releases.rollback.github_revert_hint)}
            </p>
          </div>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={submitting}>
            {t(($) => $.releases.create_dialog.cancel)}
          </AlertDialogCancel>
          <AlertDialogAction
            disabled={reason.trim() === "" || submitting}
            data-testid="release-rollback-submit"
            onClick={(e) => {
              e.preventDefault();
              void onSubmit();
            }}
          >
            {submitting
              ? t(($) => $.releases.rollback.submitting)
              : t(($) => $.releases.rollback.submit)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

/** ProductionPanels renders the production deploy summary + the
 *  health rollup. Both render through their own empty states for
 *  releases that haven't reached in_production yet. */
function ProductionPanels({
  release,
  health,
  repoUrl,
  productionPollerOn,
}: {
  release: import("@multica/core/types").Release;
  health: import("@multica/core/types").ReleaseHealth | null;
  /** Project repo URL — used to render the deployed-SHA chip as a
   *  link to the GitHub commit. Optional because empty releases
   *  (no PRs) can't supply one; the chip falls back to plain text. */
  repoUrl?: string;
  /** Phase 7d follow-up — workspace has auto-detect deploys
   *  configured for production. Same UX rationale as the staging
   *  variant: the empty-deploy copy mentions polling so the user
   *  knows the link IS being watched. */
  productionPollerOn?: boolean;
}) {
  const { t } = useT("ship");
  const sha = release.production_main_sha || release.merged_main_sha;
  return (
    <div className="grid gap-3 sm:grid-cols-2" data-testid="release-production-panels">
      <div className="rounded border bg-card p-3 text-sm">
        <div className="mb-1 flex items-center gap-1.5 text-xs font-medium uppercase tracking-wide text-muted-foreground">
          <Rocket className="size-3.5" />
          {t(($) => $.releases.production.deploy_panel_title)}
        </div>
        {release.production_deploy_id ? (
          <div className="space-y-1">
            {sha && (
              <p className="text-xs">
                <CommitLink repoUrl={repoUrl} sha={sha} />
              </p>
            )}
            {release.promoted_at && (
              <p className="text-xs text-muted-foreground">
                {t(($) => $.releases.production.deploy_at)}:{" "}
                {formatRelativeShort(release.promoted_at)}
              </p>
            )}
          </div>
        ) : (
          <p className="text-xs text-muted-foreground">
            {productionPollerOn === true
              ? t(($) => $.releases.production.awaiting_deploy_with_poller)
              : t(($) => $.releases.production.awaiting_deploy)}
          </p>
        )}
      </div>
      <HealthRollupPanel health={health} stage={release.stage} />
    </div>
  );
}

/** HealthRollupPanel — three-tier status pill + per-metric mini
 *  deltas. When `health` is null OR overall_status is "ok" with no
 *  signal, the panel renders a calm baseline state. Forward-compat:
 *  unknown status strings render the raw value (per the API drift
 *  rules). */
function HealthRollupPanel({
  health,
  stage,
}: {
  health: import("@multica/core/types").ReleaseHealth | null;
  stage: string;
}) {
  const { t } = useT("ship");
  const status = health?.overall_status ?? "ok";
  const hasAnySignal = !!health && (
    health.error_rate_delta !== null ||
    health.p99_latency_delta_ms !== null ||
    health.inbox_issues_since_promote > 0 ||
    health.agent_failure_rate_delta !== null
  );
  return (
    <div
      className="rounded border bg-card p-3 text-sm"
      data-testid="release-health-panel"
      data-overall-status={status}
    >
      <div className="mb-2 flex items-center justify-between gap-1.5 text-xs font-medium uppercase tracking-wide text-muted-foreground">
        <span className="flex items-center gap-1.5">
          <Heart className="size-3.5" />
          {t(($) => $.releases.health.panel_title)}
        </span>
        <HealthStatusPill status={status} />
      </div>
      {!hasAnySignal && stage !== "in_production" && stage !== "rolled_back" && stage !== "done" ? (
        <p className="text-xs text-muted-foreground">
          {t(($) => $.releases.health.awaiting_first_snapshot)}
        </p>
      ) : (
        <ul className="space-y-1 text-xs">
          <HealthRow
            icon={<TrendingUp className="size-3" />}
            label={t(($) => $.releases.health.error_rate_label)}
            value={formatDelta(health?.error_rate_delta ?? null, "%")}
          />
          <HealthRow
            icon={<Activity className="size-3" />}
            label={t(($) => $.releases.health.p99_latency_label)}
            value={formatDelta(health?.p99_latency_delta_ms ?? null, "ms")}
          />
          <HealthRow
            icon={<MessagesSquare className="size-3" />}
            label={t(($) => $.releases.health.inbox_label)}
            value={String(health?.inbox_issues_since_promote ?? 0)}
          />
          <HealthRow
            icon={<AlertTriangle className="size-3" />}
            label={t(($) => $.releases.health.agent_failure_label)}
            value={formatDelta(health?.agent_failure_rate_delta ?? null, "%")}
          />
        </ul>
      )}
    </div>
  );
}

function HealthRow({
  icon,
  label,
  value,
}: {
  icon: React.ReactNode;
  label: string;
  value: string;
}) {
  return (
    <li className="flex items-center justify-between gap-2 text-muted-foreground">
      <span className="flex items-center gap-1.5">
        {icon}
        {label}
      </span>
      <span className="tabular-nums text-foreground">{value}</span>
    </li>
  );
}

function HealthStatusPill({ status }: { status: string }) {
  const { t } = useT("ship");
  switch (status) {
    case "alert":
      return (
        <span className="inline-flex items-center gap-1 rounded bg-destructive/20 px-1.5 py-0.5 text-[10px] font-medium text-destructive">
          <AlertTriangle className="size-3" />
          {t(($) => $.releases.health.status_alert)}
        </span>
      );
    case "warning":
      return (
        <span className="inline-flex items-center gap-1 rounded bg-amber-500/20 px-1.5 py-0.5 text-[10px] font-medium text-amber-700 dark:text-amber-400">
          <AlertTriangle className="size-3" />
          {t(($) => $.releases.health.status_warning)}
        </span>
      );
    case "ok":
      return (
        <span className="inline-flex items-center gap-1 rounded bg-emerald-500/20 px-1.5 py-0.5 text-[10px] font-medium text-emerald-700 dark:text-emerald-400">
          <CheckCircle2 className="size-3" />
          {t(($) => $.releases.health.status_ok)}
        </span>
      );
    default:
      return (
        <span className="inline-flex items-center gap-1 rounded bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
          {status}
        </span>
      );
  }
}

/** formatDelta renders a delta with an explicit sign + unit. Returns
 *  "—" for null. Percentages multiply by 100 (the wire shape carries
 *  fractions like 0.05 = 5pp). */
function formatDelta(v: number | null, unit: "%" | "ms"): string {
  if (v === null || !Number.isFinite(v)) return "—";
  if (unit === "%") {
    const pct = v * 100;
    const sign = pct > 0 ? "+" : "";
    return `${sign}${pct.toFixed(1)}%`;
  }
  const sign = v > 0 ? "+" : "";
  return `${sign}${v.toFixed(0)}ms`;
}

/** rolledBackByLabel renders the user who rolled back. Same shape as
 *  verifiedByLabel — short hex slice as a fallback when display name
 *  isn't available. */
function rolledBackByLabel(
  release: import("@multica/core/types").Release,
): string {
  if (release.rolled_back_by) {
    return release.rolled_back_by.slice(0, 8);
  }
  return "—";
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
      // The pill itself is non-interactive; the SHA inside opens the
      // squash-merge commit on GitHub in a new tab. Click landing on
      // the SHA stops propagation so it doesn't also fire the row
      // click handler (open PR drawer).
      return (
        <span
          className="inline-flex items-center gap-1 rounded bg-emerald-500/20 px-1.5 py-0.5 text-[10px] font-medium text-emerald-700 dark:text-emerald-400"
          data-testid="release-pr-merge-state"
          data-state="merged"
        >
          <CheckCircle2 className="size-3" />
          {t(($) => $.releases.merge_state.merged)}
          {pr.merged_sha && (
            <span
              onClick={(e) => e.stopPropagation()}
              className="contents"
            >
              <CommitLink
                repoUrl={pr.repo_url}
                sha={pr.merged_sha}
                className="text-emerald-700 dark:text-emerald-400"
              />
            </span>
          )}
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
  hasStaging,
}: {
  currentStage: string;
  merging: { merged: number; total: number } | null;
  // True when the project has a `kind='staging'` deploy environment.
  // When false, the in_staging + verifying stages are filtered out of
  // the bar to match the backend flow that skips them for direct-to-
  // prod projects. Defaults to TRUE so any caller that hasn't been
  // updated to pass this prop preserves the old behavior (show all
  // 7 stages) — safer regression default.
  hasStaging?: boolean;
}) {
  const { t } = useT("ship");
  const visibleStages =
    hasStaging === false
      ? STAGE_PROGRESS.filter((s) => !STAGING_ONLY_STAGES.has(s.key))
      : STAGE_PROGRESS;
  const currentIdx = visibleStages.findIndex((s) => s.key === currentStage);
  return (
    <ol
      className={cn(
        "grid gap-1 text-[11px]",
        // Grid columns adapt to the visible count so the chips
        // re-flow evenly when staging stages are hidden.
        visibleStages.length === 7 && "grid-cols-7",
        visibleStages.length === 5 && "grid-cols-5",
      )}
      data-testid="release-stage-progress"
    >
      {visibleStages.map((s, i) => {
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

/** Resolve a deep link for a timeline event when the payload carries
 *  a navigable id. Returns null when nothing useful to link to.
 *
 *  Channel + issue events: link to the in-app channel/issue.
 *  PR add/remove + merge_train_paused: link to the PR's GitHub URL
 *    (we use the GitHub URL not the in-app card so the user goes to
 *    the diff — same place the "View diff →" chip points). External.
 *  Other events (release_verified, smoke_completed, etc.): no link.
 */
function resolveEventLink(
  event: { event_type: string; payload?: unknown },
  data: NonNullable<ReturnType<typeof useReleaseDetail>["data"]>,
  slug: string,
): { href: string; external: boolean } | null {
  const payload = (event.payload ?? {}) as Record<string, unknown>;
  const channelId = typeof payload.channel_id === "string" ? payload.channel_id : "";
  const issueId = typeof payload.issue_id === "string" ? payload.issue_id : "";
  const prId =
    typeof payload.pull_request_id === "string"
      ? payload.pull_request_id
      : typeof payload.paused_pr_id === "string"
        ? payload.paused_pr_id
        : "";

  if (channelId && slug) {
    return {
      href: `/${encodeURIComponent(slug)}/channels/${encodeURIComponent(channelId)}`,
      external: false,
    };
  }
  if (issueId && slug) {
    return {
      href: `/${encodeURIComponent(slug)}/issues/${encodeURIComponent(issueId)}`,
      external: false,
    };
  }
  if (prId) {
    // Look up the PR in the release's join data so we can route to
    // its GitHub URL (the "diff" pivot is what users want from the
    // timeline). Skip silently if the PR isn't in the release set
    // anymore (e.g. it was removed).
    const pr = data.pull_requests.find((p) => p.id === prId);
    if (pr?.html_url) return { href: pr.html_url, external: true };
  }
  return null;
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

/** CommitLink — renders a clickable monospace short-SHA chip that
 *  opens the commit on GitHub in a new tab.
 *
 *  Why: every release page section that shows a SHA used to render
 *  it as plain text. Users had no fast path from "this release is in
 *  production at ca30f18" to GitHub's commit view to verify what
 *  actually shipped. The release page is the natural ground-truth
 *  surface, but its tracking value drops sharply if every commit
 *  reference is a copy-paste dance.
 *
 *  Repo URL discovery: we accept it as a prop because the release
 *  detail response doesn't carry the project's repo URL directly.
 *  Callers derive it from `data.pull_requests[0]?.repo_url` — every
 *  PR in a release shares the same project + repo. When no repo URL
 *  is available (empty release, server drift), we render the SHA as
 *  plain text without a link. */
function CommitLink({
  repoUrl,
  sha,
  className,
}: {
  repoUrl?: string | null;
  sha: string | null | undefined;
  className?: string;
}) {
  if (!sha) return null;
  const short = sha.slice(0, 7);
  if (!repoUrl) {
    return <span className={cn("font-mono", className)}>{short}</span>;
  }
  return (
    <a
      href={`${repoUrl}/commit/${sha}`}
      target="_blank"
      rel="noopener noreferrer"
      className={cn(
        "inline-flex items-center gap-0.5 font-mono hover:underline",
        className,
      )}
      data-testid="commit-link"
    >
      {short}
      <ExternalLink className="size-3 opacity-60" aria-hidden />
    </a>
  );
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

/** Compact absolute timestamp for the timeline. Drops the year when the
 *  event is in the current year (the common case — release timelines
 *  don't span years), and renders "May 9, 3:42 PM" style.
 *
 *  Used in the event timeline where users want to know "what time did
 *  staging deploy land" without doing relative-to-absolute math from
 *  "17h ago". The relative form is preserved as a hover tooltip via
 *  the `title` attribute on the <time> element so both views are one
 *  cursor-hover apart. */
function formatAbsoluteShort(iso: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (!Number.isFinite(d.getTime())) return "";
  const now = new Date();
  const sameYear = d.getFullYear() === now.getFullYear();
  return d.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    year: sameYear ? undefined : "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

/** Resolve the effective approval rule for a release's risk tier.
 *  Reads `workspace.ship_hub_approval_<tier>` and falls back to the
 *  legacy defaults (low/medium → member, high → approver, critical →
 *  two) when the workspace setting is empty.
 *
 *  Kept in sync with the server-side `resolveApprovalRule` in
 *  `service/ship/release_staging.go`. If you change the defaults here,
 *  change them there. */
function approvalRuleForRisk(
  workspace: ReturnType<typeof useCurrentWorkspace>,
  risk: string,
): "member" | "admin" | "approver" | "two" {
  const norm = (
    v: string | undefined,
    fallback: "member" | "admin" | "approver" | "two",
  ): "member" | "admin" | "approver" | "two" => {
    switch (v) {
      case "member":
      case "admin":
      case "approver":
      case "two":
        return v;
    }
    return fallback;
  };
  if (!workspace) return "member";
  switch (risk) {
    case "low":
      return norm(workspace.ship_hub_approval_low, "member");
    case "medium":
      return norm(workspace.ship_hub_approval_medium, "member");
    case "high":
      return norm(workspace.ship_hub_approval_high, "approver");
    case "critical":
      return norm(workspace.ship_hub_approval_critical, "two");
    default:
      return "member";
  }
}

/** checkStartMergePreconditions returns a list of human-readable
 *  reasons the start_merge button should be disabled, or [] when
 *  it's safe to start. We intentionally surface these on the UI
 *  side too (the server enforces them at start_merge time) so the
 *  user sees why the button is greyed out before clicking.
 *
 *  Approver gate respects the workspace's per-risk-tier approval rule
 *  (`ship_hub_approval_<tier>`). With the default for medium = "member",
 *  no approver is required. Setting medium = "approver" restores the
 *  old behavior. Critical defaults to "two" so the two-approver
 *  requirement still bites unless the workspace explicitly opts out. */
function checkStartMergePreconditions(
  data: NonNullable<ReturnType<typeof useReleaseDetail>["data"]>,
  workspace: ReturnType<typeof useCurrentWorkspace>,
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
  const rule = approvalRuleForRisk(workspace, data.release.risk_level);
  if (rule === "approver" && !data.release.approver_id) {
    reasons.push(`Risk ${data.release.risk_level} requires an approver`);
  }
  if (rule === "two") {
    if (!data.release.approver_id) {
      reasons.push(`Risk ${data.release.risk_level} requires an approver`);
    }
    if (!data.release.second_approver_id) {
      reasons.push(`Risk ${data.release.risk_level} requires a second approver`);
    }
  }
  return reasons;
}

// ---------------------------------------------------------------------------
// Phase 7c — staging-stage components.
// ---------------------------------------------------------------------------

/** Phase 7c polish — "Next step" banner that surfaces the gating
 *  condition + the user's available action when the release is in
 *  the staging stages. Without this the release page can look halted
 *  to the user: the merge train completed, the page shows "IN STAGING",
 *  and there's no obvious "what now?". The banner names exactly what
 *  the system is waiting on and what the user can press to advance.
 *
 *  States covered:
 *    in_staging + no merged_main_sha   → "Can't link a deploy to this
 *                                         release; mark verified to
 *                                         advance manually."
 *    in_staging + no staging_deploy_id → "Waiting for the staging
 *                                         deploy of {sha} to land.
 *                                         Or mark verified manually."
 *    in_staging + smoke=in_progress    → "Smoke tests running…"
 *    in_staging + smoke=completed_failure → "Smoke failed; review +
 *                                            re-run, manual-pass, or
 *                                            mark verified."
 *    in_staging + smoke=passing/skipped → "Ready to mark verified."
 *    verifying                         → "Verified. Awaiting
 *                                         promotion to production."
 */
function NextStepBanner({
  release,
  stagingPollerOn,
  productionPollerOn,
}: {
  release: import("@multica/core/types").Release;
  /** True when the workspace has configured an auto-detect staging
   *  deploy workflow. Swaps the "awaiting deploy" copy to mention
   *  the polling cadence so the user knows the link IS being watched
   *  rather than wondering if the feature is broken. */
  stagingPollerOn?: boolean;
  /** Same as stagingPollerOn, for production deploys. We don't
   *  currently render a "promoting awaiting prod deploy" banner here
   *  (the promoting copy is generic), but the prop is plumbed for
   *  symmetry / future use. */
  productionPollerOn?: boolean;
}) {
  const { t } = useT("ship");
  const stage = release.stage;
  const smoke = release.smoke_status ?? "";
  const hasSha = !!release.merged_main_sha;
  const hasDeploy = !!release.staging_deploy_id;
  // Suppress the unused-var warning until we wire productionPollerOn
  // into the promoting branch. Stays referenced for forward-compat.
  void productionPollerOn;

  let message = "";
  let testId = "release-next-step-default";

  if (stage === "promoting") {
    message = t(($) => $.releases.next_step.promoting);
    testId = "release-next-step-promoting";
  } else if (stage === "in_production") {
    message = t(($) => $.releases.next_step.in_production);
    testId = "release-next-step-in-production";
  } else if (stage === "rolled_back") {
    message = t(($) => $.releases.next_step.rolled_back);
    testId = "release-next-step-rolled-back";
  } else if (stage === "done") {
    message = t(($) => $.releases.next_step.done);
    testId = "release-next-step-done";
  } else if (stage === "verifying") {
    message = t(($) => $.releases.next_step.verifying);
    testId = "release-next-step-verifying";
  } else if (!hasSha) {
    message = t(($) => $.releases.next_step.no_merged_sha);
    testId = "release-next-step-no-sha";
  } else if (!hasDeploy) {
    // When the workspace has the auto-detect poller configured, swap
    // the copy from "click Mark deploy as landed" to "polling, link
    // should land within 4min" — signals to the user that the wait is
    // expected, not a broken state.
    message = t(
      ($) =>
        stagingPollerOn === true
          ? $.releases.next_step.awaiting_deploy_with_poller
          : $.releases.next_step.awaiting_deploy,
      {
        sha: release.merged_main_sha?.slice(0, 7) ?? "",
      },
    );
    testId = "release-next-step-awaiting-deploy";
  } else if (smoke === "in_progress" || smoke === "queued") {
    message = t(($) => $.releases.next_step.smoke_running);
    testId = "release-next-step-smoke-running";
  } else if (smoke === "completed_failure") {
    message = t(($) => $.releases.next_step.smoke_failed);
    testId = "release-next-step-smoke-failed";
  } else if (
    smoke === "completed_success" ||
    smoke === "manual_pass" ||
    smoke === "skipped" ||
    smoke === ""
  ) {
    message = t(($) => $.releases.next_step.ready_to_verify);
    testId = "release-next-step-ready";
  }

  if (!message) return null;
  return (
    <div
      className="flex items-start gap-2 rounded border border-blue-500/40 bg-blue-500/10 p-3 text-sm text-blue-700 dark:text-blue-300"
      data-testid={testId}
    >
      <CornerDownRight className="mt-0.5 size-4 shrink-0" aria-hidden />
      <p className="flex-1">{message}</p>
    </div>
  );
}

/** StagingActionButtons renders the in_staging / verifying button row.
 *  We split this out of the header because the parent's button list
 *  was already heavy with merge-train branches; keeping the staging
 *  affordances colocated makes the gating logic easier to follow. */
function StagingActionButtons({
  release,
  smokeWorkflowConfigured,
  onRunSmoke,
  runSmokePending,
  onMarkSmokePass,
  onMarkVerified,
  onUnverify,
  onMarkStaged,
  markStagedPending,
}: {
  release: import("@multica/core/types").Release;
  /** True when the workspace has a smoke-test workflow configured.
   *  When false, the "Run smoke tests" button is hidden entirely
   *  (clicking it would 400 anyway) and "Manual pass" is the only
   *  way to advance the smoke gate. */
  smokeWorkflowConfigured: boolean;
  onRunSmoke: () => void;
  runSmokePending: boolean;
  onMarkSmokePass: () => void;
  onMarkVerified: () => void;
  onUnverify: () => void;
  /** Manual escape hatch: synthesize a successful staging deploy with
   *  the release's merged_main_sha. Only meaningful when stage is
   *  in_staging AND staging_deploy_id IS NULL AND merged_main_sha
   *  is set. The button hides otherwise. */
  onMarkStaged: () => void;
  markStagedPending: boolean;
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

  // Phase 7c polish — show the "Mark deploy as landed" affordance
  // whenever the release is in_staging without a linked deploy AND
  // has a merged_main_sha to deploy. This is the manual escape
  // hatch for repos whose CI doesn't fire GitHub deployment_status
  // events (Vercel/Netlify/Cloudflare/custom CI).
  const canMarkStaged =
    !isVerifying &&
    !release.staging_deploy_id &&
    !!release.merged_main_sha;

  return (
    <>
      {canMarkStaged && (
        <Button
          size="sm"
          onClick={onMarkStaged}
          disabled={markStagedPending}
          data-testid="release-mark-staged-button"
        >
          <Rocket className="size-3.5" />
          {t(($) => $.releases.staging.mark_staged_button)}
        </Button>
      )}
      {/* Run smoke is the primary affordance whenever it's not
          mid-flight; the "Re-run" copy applies after a completed run.
          Phase 7c polish — hidden entirely when the workspace hasn't
          configured a smoke workflow. Pressing it would return 400
          ("smoke workflow not configured for this workspace") so the
          button being live was a UX trap. With the affordance gone,
          Manual pass becomes the explicit path. */}
      {smokeWorkflowConfigured && (
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
      )}
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
  repoUrl,
  smokeWorkflowConfigured,
  stagingPollerOn,
}: {
  release: import("@multica/core/types").Release;
  /** Project repo URL — used to render the deployed-SHA chip as a
   *  link to the GitHub commit. Optional; falls back to plain text. */
  repoUrl?: string;
  smokeWorkflowConfigured: boolean;
  /** Phase 7d follow-up — workspace has auto-detect deploys
   *  configured for staging. When true, the empty-deploy copy mentions
   *  the polling cadence so the user understands "the link IS being
   *  watched" instead of treating the empty state as broken. */
  stagingPollerOn?: boolean;
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
              <p className="text-xs">
                <CommitLink repoUrl={repoUrl} sha={release.merged_main_sha} />
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
            {stagingPollerOn === true
              ? t(($) => $.releases.staging.awaiting_deploy_with_poller)
              : t(($) => $.releases.staging.awaiting_deploy)}
          </p>
        )}
      </div>

      {/* Smoke panel. Phase 7c polish — when the workspace hasn't
          configured a smoke workflow, render an explicit "Not
          configured" state with a settings link instead of an empty
          dash, so the user understands smoke is optional rather than
          broken. */}
      <div
        className="rounded border bg-card p-3 text-sm"
        data-testid="release-smoke-panel"
        data-smoke-status={smoke}
      >
        <div className="mb-1 flex items-center gap-1.5 text-xs font-medium uppercase tracking-wide text-muted-foreground">
          <FlaskConical className="size-3.5" />
          {t(($) => $.releases.staging.smoke_panel_title)}
        </div>
        {!smokeWorkflowConfigured && smoke === "" ? (
          <p className="text-xs text-muted-foreground">
            {t(($) => $.releases.staging.smoke_not_configured)}
          </p>
        ) : (
          <SmokeStatusPill status={smoke} />
        )}
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

/** pendingSecondApproverName picks the still-needed approver's
 *  short identifier for the "awaiting second approver" banner. The
 *  release row carries the two slots as UUIDs (approver_id +
 *  second_approver_id); the signoffs array tells us which slot is
 *  filled. We slice to 8 chars for the same reason verifiedByLabel
 *  does — the row only carries UUIDs. The full identity comes from
 *  the audit log when the user clicks through. */
function pendingSecondApproverName(
  release: import("@multica/core/types").Release,
  signoffs: import("@multica/core/types").ReleaseSignoff[],
): string {
  const filledFirst = signoffs.some((s) => s.approver_slot === "first");
  // Pending = the slot WITHOUT a signoff. If first is filled, we're
  // waiting on the second slot; otherwise the first.
  const pendingId = filledFirst ? release.second_approver_id : release.approver_id;
  if (pendingId) return pendingId.slice(0, 8);
  return "—";
}

/** Manual "Open discussion channel" button for the release detail
 *  page. Renders in place of the linked-channel chip when the release
 *  has no channel yet. Navigates to the new channel on success.
 *
 *  Replaces the auto-create-on-CreateRelease path: most releases ship
 *  without needing a chat surface (the tracking issue covers the
 *  decisions / state / rollback record). The user clicks here only
 *  when broad-team chat actually warrants it (multi-day verifies,
 *  high-stakes rollouts). */
function OpenReleaseChannelButton({ releaseId }: { releaseId: string }) {
  const { t } = useT("ship");
  const open = useOpenReleaseChannel(releaseId);
  const navigation = useNavigation();
  const workspace = useCurrentWorkspace();
  const slug = workspace?.slug ?? "";

  const handleClick = async () => {
    try {
      const res = (await open.mutateAsync()) as { name?: string } | null;
      const name = res?.name;
      if (slug && name) {
        navigation.push(`/${slug}/channels/${name}`);
      }
    } catch (err) {
      toast.error(friendlyMutationError(err));
    }
  };

  return (
    <Button
      type="button"
      variant="outline"
      size="sm"
      className="inline-flex items-center gap-1.5"
      onClick={handleClick}
      disabled={open.isPending}
      data-testid="release-open-channel"
    >
      <MessagesSquare className="size-3.5" aria-hidden />
      {open.isPending
        ? t(($) => $.releases.detail.opening_channel)
        : t(($) => $.releases.detail.open_discussion_channel)}
    </Button>
  );
}
