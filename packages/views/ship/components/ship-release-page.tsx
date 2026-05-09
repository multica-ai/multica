"use client";

// Phase 7a — Release detail page.
//
// Shared between web and desktop via `<ShipReleasePage releaseId={…} />`.
// Renders:
//   * Header: title, stage badge, risk badge, project, created-by
//   * Stage progress bar (8 visual stages, current highlighted)
//   * PR table with per-PR remove (assembling stage only)
//   * Linked channel + issue chips
//   * Timeline (release_event log)
//   * Action bar (Edit / Add PR / Cancel — context-sensitive to stage)

import { useState } from "react";
import { toast } from "sonner";
import {
  AlertTriangle,
  CheckCircle2,
  Hash,
  MessagesSquare,
  Rocket,
  Train,
  X,
} from "lucide-react";
import {
  useReleaseDetail,
  useCancelRelease,
  useRemovePullRequestFromRelease,
} from "@multica/core/ship";
import { useCurrentWorkspace } from "@multica/core/paths";
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
  const [cancelOpen, setCancelOpen] = useState(false);
  const [cancelReason, setCancelReason] = useState("");

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
  const isCancelled = release.stage === "cancelled";
  const isRolledBack = release.stage === "rolled_back";
  const slug = workspace?.slug ?? "";

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
            {release.approver_id && (
              <span className="text-xs text-muted-foreground">
                {t(($) => $.releases.detail.approver_label)}:{" "}
                <code className="text-foreground">{release.approver_id.slice(0, 8)}</code>
              </span>
            )}
            <div className="ml-auto flex items-center gap-2">
              {isAssembling && (
                <Button
                  size="sm"
                  variant="destructive"
                  onClick={() => setCancelOpen(true)}
                  data-testid="release-cancel-button"
                >
                  <X className="size-3.5" />
                  {t(($) => $.releases.detail.cancel_release)}
                </Button>
              )}
            </div>
          </header>

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
            <StageProgressBar currentStage={release.stage} />
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
                      {pr.merged_sha && (
                        <CheckCircle2 className="size-3.5 text-emerald-500" />
                      )}
                    </a>
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
    </div>
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

function StageProgressBar({ currentStage }: { currentStage: string }) {
  const { t } = useT("ship");
  const currentIdx = STAGE_PROGRESS.findIndex((s) => s.key === currentStage);
  return (
    <ol
      className="grid grid-cols-7 gap-1 text-[11px]"
      data-testid="release-stage-progress"
    >
      {STAGE_PROGRESS.map((s, i) => {
        const reached = currentIdx >= 0 && i <= currentIdx;
        return (
          <li
            key={s.key}
            className={cn(
              "rounded px-1.5 py-1 text-center transition-colors",
              reached ? s.iconBg : "bg-muted/40 text-muted-foreground",
              i === currentIdx && "ring-1 ring-primary",
            )}
          >
            {t(($) =>
              ($.releases.stage as Record<string, string>)[s.key] ??
              $.releases.stage.assembling,
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
