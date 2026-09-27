"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  CheckCircle2,
  Circle,
  CircleDashed,
  CircleSlash,
  Clock,
  GitMerge,
  LoaderCircle,
  MoreHorizontal,
  Unlink,
  GitPullRequest,
  GitPullRequestArrow,
  GitPullRequestClosed,
  GitPullRequestDraft,
  TriangleAlert,
  XCircle,
} from "lucide-react";
import {
  issuePullRequestsOptions,
  derivePullRequestVerdict,
  formatPullRequestDiffCount,
  shouldShowPullRequestStats,
  stripIssueKeyFromTitle,
  useLinkIssuePullRequest,
  useSetIssuePRAutoComplete,
  useUnlinkIssuePullRequest,
  type PullRequestVerdict,
} from "@multica/core/github";
import { useWorkspaceId } from "@multica/core/hooks";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import type {
  GitHubPullRequest,
  GitHubPullRequestState,
  PRAutoComplete,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import { useT, useTimeAgo } from "../../i18n";
import { useStatusLabel } from "../utils/status-label";
import { StatusIcon } from "./status-icon";

type IssuesT = ReturnType<typeof useT<"issues">>["t"];


// Keep the existing sidebar density: show the first 3 PR rows inline, then
// collapse the rest once the section reaches 4 rows.
const PR_LIMIT_BEFORE_COLLAPSE = 4;

const STATE_ICON: Record<
  GitHubPullRequestState,
  { icon: React.ComponentType<{ className?: string }>; className: string }
> = {
  open: { icon: GitPullRequestArrow, className: "text-emerald-600 dark:text-emerald-400" },
  draft: { icon: GitPullRequestDraft, className: "text-muted-foreground" },
  merged: { icon: GitMerge, className: "text-violet-600 dark:text-violet-400" },
  closed: { icon: GitPullRequestClosed, className: "text-rose-600 dark:text-rose-400" },
};

export function PullRequestList({
  issueId,
  identifier = "",
}: {
  issueId: string;
  /** The issue's identifier, for the "linked by MUL-1 in the title" label. */
  identifier?: string;
}) {
  const { t } = useT("issues");
  const [expanded, setExpanded] = useState(false);
  const { data, isLoading } = useQuery(issuePullRequestsOptions(issueId));
  const prs = data?.pull_requests ?? [];
  // Older backends send no auto-complete block and have no link/unlink
  // endpoints, so the row actions stay hidden with it.
  const autoComplete = data?.auto_complete ?? null;
  const rowActions = autoComplete ? { issueId, identifier, autoComplete } : null;

  if (isLoading) {
    return <p className="text-caption text-muted-foreground px-2">{t(($) => $.detail.pull_requests_loading)}</p>;
  }
  if (prs.length === 0) {
    return (
      <p className="px-2 text-caption text-muted-foreground">
        {t(($) => $.detail.pull_requests_empty_title)}
      </p>
    );
  }

  // Render rule:
  //   - <  PR_LIMIT_BEFORE_COLLAPSE: every PR row is visible.
  //   - >= PR_LIMIT_BEFORE_COLLAPSE: first (LIMIT - 1) rows are visible and
  //     the remainder sits behind a toggle.
  const useCollapse = prs.length >= PR_LIMIT_BEFORE_COLLAPSE;
  const expandedHead = useCollapse ? prs.slice(0, PR_LIMIT_BEFORE_COLLAPSE - 1) : prs;
  const collapsedTail = useCollapse ? prs.slice(PR_LIMIT_BEFORE_COLLAPSE - 1) : [];

  return (
    <div className="space-y-1">
      {expandedHead.map((pr) => (
        <PullRequestRow key={pr.id} pr={pr} identifier={identifier} actions={rowActions} />
      ))}
      {useCollapse ? (
        <div className="space-y-1">
          {expanded
            ? collapsedTail.map((pr) => (
                <PullRequestRow key={pr.id} pr={pr} identifier={identifier} actions={rowActions} />
              ))
            : null}
          <button
            type="button"
            onClick={() => setExpanded((v) => !v)}
            className="block w-[calc(100%+1rem)] -mx-2 rounded-md px-2 py-1.5 text-left text-micro text-muted-foreground hover:bg-accent/50 hover:text-foreground transition-colors"
          >
            {expanded
              ? t(($) => $.detail.pull_request_card_show_less)
              : t(($) => $.detail.pull_request_card_show_more, { count: collapsedTail.length })}
          </button>
        </div>
      ) : null}
      {autoComplete ? (
        <AutoCompleteLine issueId={issueId} prs={prs} autoComplete={autoComplete} />
      ) : null}
    </div>
  );
}


interface RowActions {
  issueId: string;
  identifier: string;
  autoComplete: PRAutoComplete;
}

const prLabel = (pr: Pick<GitHubPullRequest, "number">) => `#${pr.number}`;

/** Where a merge moves the issue; older backends only ever moved it to Done. */
const mergeTarget = (autoComplete: PRAutoComplete) => autoComplete.target_status ?? "done";

/** States in which the merge has nothing left to do for this issue. */
const SETTLED = new Set(["terminal", "at_target"]);

/**
 * Remove a PR from the issue. The server remembers the removal (webhooks will
 * not link it again) and treats it as a PR event, so removing the last
 * unmerged PR can move the issue — the toast says so, and offers undo only
 * when nothing else changed.
 */
function useUnlinkPullRequest(issueId: string, before: PRAutoComplete) {
  const { t } = useT("issues");
  const statusLabel = useStatusLabel(useWorkspaceId());
  const unlink = useUnlinkIssuePullRequest(issueId);
  const relink = useLinkIssuePullRequest(issueId);
  return (pr: GitHubPullRequest) => {
    unlink.mutate(pr.id, {
      onSuccess: (after) => {
        const moved =
          !SETTLED.has(before.state) && !!after.auto_complete && SETTLED.has(after.auto_complete.state);
        if (moved) {
          toast.success(
            t(($) => $.pr_automation.unlinked_moved, {
              pr: prLabel(pr),
              status: statusLabel(mergeTarget(after.auto_complete!)),
            }),
          );
          return;
        }
        toast.success(t(($) => $.pr_automation.unlinked, { pr: prLabel(pr) }), {
          action: {
            label: t(($) => $.pr_automation.undo),
            onClick: () => relink.mutate({ pull_request_id: pr.id }),
          },
        });
      },
      onError: () => toast.error(t(($) => $.pr_automation.unlink_failed)),
    });
  };
}

function linkSourceLabel(pr: GitHubPullRequest, identifier: string, t: IssuesT): string {
  switch (pr.link_source) {
    case "manual":
      return t(($) => $.pr_automation.source_manual);
    case "title":
      return t(($) => $.pr_automation.source_title, { identifier });
    case "branch":
      return t(($) => $.pr_automation.source_branch);
    default:
      return t(($) => $.pr_automation.source_auto);
  }
}

function PullRequestRowMenu({ pr, actions }: { pr: GitHubPullRequest; actions: RowActions }) {
  const { t } = useT("issues");
  const unlink = useUnlinkPullRequest(actions.issueId, actions.autoComplete);
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="ghost"
            size="icon-xs"
            aria-label={t(($) => $.pr_automation.row_menu)}
            className="absolute top-0.5 right-0 opacity-0 group-hover/pr:opacity-100 group-focus-within/pr:opacity-100 data-popup-open:opacity-100 [@media(pointer:coarse)]:opacity-100"
          >
            <MoreHorizontal />
          </Button>
        }
      />
      <DropdownMenuContent align="end" className="w-56">
        <DropdownMenuGroup>
          <DropdownMenuLabel className="font-normal">
            {linkSourceLabel(pr, actions.identifier, t)}
          </DropdownMenuLabel>
          <DropdownMenuItem onClick={() => unlink(pr)}>
            <Unlink />
            {t(($) => $.pr_automation.unlink)}
          </DropdownMenuItem>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

// Splits a translated sentence around the target status so each language keeps
// its own word order and the status renders as a chip, not plain text.
const STATUS_SLOT = "\u2063status\u2063";

/**
 * One line under the PR list saying what "every linked PR merged → move the
 * issue to the workspace's target status" will do for this issue, straight
 * from the server's decision. It speaks only when a merge would move the issue
 * or this issue opted out: a workspace that leaves status alone, a finished or
 * triaged issue, one already in the target, and unknown states render nothing.
 */
function AutoCompleteLine({
  issueId,
  prs,
  autoComplete,
}: {
  issueId: string;
  prs: GitHubPullRequest[];
  autoComplete: PRAutoComplete;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const catalog = useIssueStatuses(wsId);
  const statusLabel = useStatusLabel(wsId);
  const unlink = useUnlinkPullRequest(issueId, autoComplete);
  const setAutoComplete = useSetIssuePRAutoComplete(issueId);
  const named = autoComplete.pull_request_ids
    .map((id) => prs.find((pr) => pr.id === id))
    .filter((pr): pr is GitHubPullRequest => !!pr);
  const list = named.map(prLabel).join(", ");
  const action = (label: string, onClick: () => void) => (
    <button
      type="button"
      onClick={onClick}
      className="font-medium text-foreground underline decoration-border underline-offset-2 hover:decoration-foreground"
    >
      {label}
    </button>
  );
  const target = mergeTarget(autoComplete);
  const withTarget = (sentence: string) => {
    const [before, after = ""] = sentence.split(STATUS_SLOT);
    return (
      <>
        {before}
        <span className="inline-flex items-center gap-1 align-[-2px] font-medium text-foreground">
          <StatusIcon
            status={target}
            category={catalog.categoryOf(target)}
            color={catalog.colorOf(target)}
            icon={catalog.iconOf(target)}
            className="size-3"
          />
          {statusLabel(target)}
        </span>
        {after}
      </>
    );
  };

  let icon: React.ReactNode;
  let body: React.ReactNode;
  switch (autoComplete.state) {
    case "waiting":
      if (named.length === 0) return null;
      icon = <CircleDashed className="text-muted-foreground" />;
      body = withTarget(t(($) => $.pr_automation.waiting_to, { count: named.length, prs: list, status: STATUS_SLOT }));
      break;
    case "not_merged": {
      if (named.length === 0) return null;
      const only = named.length === 1 ? named[0] : undefined;
      icon = <TriangleAlert className="text-amber-600 dark:text-amber-400" />;
      body = (
        <>
          {t(($) => $.pr_automation.not_merged, { prs: list })}
          {only ? <> · {action(t(($) => $.pr_automation.not_merged_action), () => unlink(only))}</> : null}
        </>
      );
      break;
    }
    case "all_merged":
      icon = <CheckCircle2 className="text-muted-foreground" />;
      body = withTarget(t(($) => $.pr_automation.all_merged_to, { status: STATUS_SLOT }));
      break;
    case "issue_disabled":
      icon = <CircleSlash className="text-muted-foreground" />;
      body = (
        <>
          {t(($) => $.pr_automation.issue_disabled)} ·{" "}
          {action(t(($) => $.pr_automation.issue_disabled_action), () =>
            setAutoComplete.mutate(false, {
              onError: () => toast.error(t(($) => $.pr_automation.update_failed)),
            }),
          )}
        </>
      );
      break;
    default:
      return null;
  }
  return (
    <p
      data-testid="pr-auto-complete-line"
      className="mt-1 flex items-start gap-1.5 border-t border-border pt-2 text-caption text-muted-foreground [&>svg]:mt-0.5 [&>svg]:size-3.5 [&>svg]:shrink-0"
    >
      {icon}
      <span className="min-w-0">{body}</span>
    </p>
  );
}


// At most this many problem lines under a failed PR; the rest fold into "+N more".
const PROBLEM_LIMIT = 3;

type VerdictTone = "red" | "amber" | "green" | "violet" | "muted";

// Pills reuse the state icons' palette. Their text sits one shade darker so
// 11px copy on a 10% tint keeps AA contrast.
const TONE_CLASS: Record<VerdictTone, string> = {
  red: "bg-rose-600/10 text-rose-700 dark:bg-rose-400/15 dark:text-rose-400",
  amber: "bg-amber-600/10 text-amber-700 dark:bg-amber-400/15 dark:text-amber-400",
  green: "bg-emerald-600/10 text-emerald-700 dark:bg-emerald-400/15 dark:text-emerald-400",
  violet: "bg-violet-600/10 text-violet-700 dark:bg-violet-400/15 dark:text-violet-400",
  muted: "bg-muted text-muted-foreground",
};

interface VerdictPillConfig {
  icon: React.ComponentType<{ className?: string }>;
  tone: VerdictTone;
  label: string;
  /** The longer form of the label, on hover. */
  title?: string;
}

/**
 * One PR in the sidebar: the title, then `repo#number` with the diff size and
 * one verdict pill aligned right, so several PRs scan as a column. A failed PR
 * also lists what failed. Owner, author, the full title and exact counts live
 * in the row's tooltip.
 */
function PullRequestRow({
  pr,
  identifier,
  actions,
}: {
  pr: GitHubPullRequest;
  identifier: string;
  actions: RowActions | null;
}) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const cfg = STATE_ICON[pr.state] ?? { icon: GitPullRequest, className: "" };
  const StateIcon = cfg.icon;
  const verdict = derivePullRequestVerdict(pr);
  const pill = getVerdictPill(verdict, t);
  const showStats = shouldShowPullRequestStats(pr);

  // A stale snapshot (GitHub outage / revoked key) greys out the verdict and
  // shows the snapshot age instead of hiding the last-known data.
  const isTerminal = pr.state === "merged" || pr.state === "closed";
  const stale = !isTerminal && pr.snapshot_stale === true;
  const staleTitle = stale
    ? pr.snapshot_fetched_at
      ? t(($) => $.detail.pull_request_snapshot_stale, { time: timeAgo(pr.snapshot_fetched_at) })
      : t(($) => $.detail.pull_request_snapshot_stale_unknown)
    : undefined;

  const endedAt = pr.state === "merged" ? pr.merged_at : pr.state === "closed" ? pr.closed_at : null;
  let meta: React.ReactNode = null;
  if (isTerminal) {
    meta = endedAt ? <span className="truncate">{timeAgo(endedAt)}</span> : null;
  } else if (stale && pr.snapshot_fetched_at) {
    meta = (
      <span className="inline-flex min-w-0 items-center gap-1" title={staleTitle}>
        <Clock className="size-3 shrink-0" />
        <span className="truncate">{timeAgo(pr.snapshot_fetched_at)}</span>
      </span>
    );
  } else if (showStats) {
    // The diff yields first when the pill needs the room; repo#number never does.
    meta = (
      <span className="min-w-0 overflow-hidden">
        <span className="text-emerald-600 dark:text-emerald-400">+{formatPullRequestDiffCount(pr.additions ?? 0)}</span>{" "}
        <span className="text-rose-600 dark:text-rose-400">−{formatPullRequestDiffCount(pr.deletions ?? 0)}</span>
      </span>
    );
  }

  // The row link and its menu are siblings: a button inside an anchor is
  // invalid HTML, and the menu must not open the PR.
  return (
    <div className="group/pr relative -mx-2 rounded-md transition-colors hover:bg-accent/50">
      <Tooltip>
        <TooltipTrigger
          render={
            <a
              data-testid="pull-request-row"
              href={pr.html_url}
              target="_blank"
              rel="noreferrer noopener"
              className="flex items-start gap-2 rounded-md px-2 py-1.5"
            />
          }
        >
          <StateIcon className={cn("mt-px size-3.5 shrink-0", cfg.className)} />
          <div className="min-w-0 flex-1">
            <p className={cn("truncate text-caption font-medium", actions ? "pr-5" : null)}>
              {stripIssueKeyFromTitle(pr.title, identifier)}
            </p>
            <div className="mt-1 flex items-center gap-2">
              <p className="flex min-w-0 items-center gap-1.5 whitespace-nowrap text-micro text-muted-foreground tabular-nums">
                <span className="shrink-0">
                  {pr.repo_name}#{pr.number}
                </span>
                {meta ? (
                  <>
                    <span aria-hidden="true" className="shrink-0 text-faint-foreground">
                      ·
                    </span>
                    {meta}
                  </>
                ) : null}
              </p>
              {pill ? <VerdictPill pill={pill} stale={stale} title={staleTitle} /> : null}
            </div>
            {verdict.kind === "failed" ? <PullRequestProblems verdict={verdict} stale={stale} /> : null}
          </div>
        </TooltipTrigger>
        <TooltipContent side="left" align="start" className="flex-col items-start gap-0.5">
          <span className="font-medium">{pr.title}</span>
          <span className="text-muted-foreground">
            {pr.repo_owner}/{pr.repo_name}#{pr.number}
            {pr.author_login ? ` · @${pr.author_login}` : null}
          </span>
          {showStats ? (
            <span className="text-muted-foreground tabular-nums">
              +{(pr.additions ?? 0).toLocaleString()} −{(pr.deletions ?? 0).toLocaleString()} ·{" "}
              {t(($) => $.detail.pull_request_card_files_count, { count: pr.changed_files ?? 0 })}
            </span>
          ) : null}
        </TooltipContent>
      </Tooltip>
      {actions ? <PullRequestRowMenu pr={pr} actions={actions} /> : null}
    </div>
  );
}

function VerdictPill({ pill, stale, title }: { pill: VerdictPillConfig; stale: boolean; title?: string }) {
  const Icon = pill.icon;
  return (
    <span
      data-testid="pull-request-verdict"
      title={title ?? pill.title}
      className={cn(
        "ml-auto inline-flex h-4.5 shrink-0 items-center gap-1 whitespace-nowrap rounded-full px-1.5 text-micro font-medium tabular-nums",
        TONE_CLASS[pill.tone],
        stale ? "opacity-60" : null,
      )}
    >
      <Icon className="size-3 shrink-0" />
      {pill.label}
    </span>
  );
}

/** What stands in the way of a failed PR: a conflict, then each failing check. */
function PullRequestProblems({
  verdict,
  stale,
}: {
  verdict: Extract<PullRequestVerdict, { kind: "failed" }>;
  stale: boolean;
}) {
  const { t } = useT("issues");
  const items = [
    ...(verdict.conflicting
      ? [{ key: "conflict", icon: TriangleAlert, className: "text-amber-600 dark:text-amber-400", label: t(($) => $.detail.pull_request_merge_conflicting) }]
      : []),
    ...verdict.names.map((name) => ({ key: `check:${name}`, icon: XCircle, className: "text-rose-600 dark:text-rose-400", label: name })),
  ];
  if (items.length === 0) return null;
  const shown = items.slice(0, PROBLEM_LIMIT);
  const remaining = items.length - shown.length;
  return (
    <ul className={cn("mt-1.5 rounded-md bg-muted/70 px-2 py-1 text-micro", stale ? "opacity-60" : null)}>
      {shown.map(({ key, icon: Icon, className, label }) => (
        <li key={key} className="flex items-center gap-1.5 py-px">
          <Icon className={cn("size-3 shrink-0", className)} />
          <span className="truncate">{label}</span>
        </li>
      ))}
      {remaining > 0 ? (
        <li className="py-px pl-4.5 text-muted-foreground">
          {t(($) => $.detail.pull_request_checks_more, { count: remaining })}
        </li>
      ) : null}
    </ul>
  );
}

function getVerdictPill(verdict: PullRequestVerdict, t: IssuesT): VerdictPillConfig | null {
  switch (verdict.kind) {
    case "merged":
      return { icon: GitMerge, tone: "violet", label: t(($) => $.detail.pull_request_state_merged) };
    case "closed":
      return { icon: GitPullRequestClosed, tone: "muted", label: t(($) => $.detail.pull_request_state_closed) };
    case "failed":
      return {
        icon: XCircle,
        tone: "red",
        label: t(($) => $.detail.pull_request_checks_failed_count, {
          failed: verdict.failed,
          total: verdict.total,
        }),
      };
    case "conflicting":
      return {
        icon: TriangleAlert,
        tone: "amber",
        label: t(($) => $.detail.pull_request_verdict_conflicting),
        title: t(($) => $.detail.pull_request_merge_conflicting),
      };
    case "running":
      return {
        icon: LoaderCircle,
        tone: "amber",
        label: `${verdict.passed}/${verdict.total}`,
        title: t(($) => $.detail.pull_request_checks_running, {
          passed: verdict.passed,
          total: verdict.total,
          running: verdict.running,
        }),
      };
    case "behind":
      return { icon: CircleSlash, tone: "muted", label: t(($) => $.detail.pull_request_merge_behind) };
    case "blocked":
      return { icon: CircleSlash, tone: "muted", label: t(($) => $.detail.pull_request_merge_blocked) };
    case "draft":
      return { icon: GitPullRequestDraft, tone: "muted", label: t(($) => $.detail.pull_request_state_draft) };
    case "ready":
      return {
        icon: CheckCircle2,
        tone: "green",
        label: t(($) => $.detail.pull_request_verdict_ready),
        title: t(($) => $.detail.pull_request_merge_ready),
      };
    case "passed":
      return {
        icon: CheckCircle2,
        tone: "green",
        label: t(($) => $.detail.pull_request_verdict_passed),
        title: t(($) => $.detail.pull_request_checks_all_passed, { total: verdict.total }),
      };
    case "no_checks":
      return { icon: Circle, tone: "muted", label: t(($) => $.detail.pull_request_checks_none) };
    case "unknown":
      return null;
  }
}
