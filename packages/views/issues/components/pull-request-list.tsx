"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  CheckCircle2,
  Circle,
  CircleDashed,
  CircleSlash,
  GitMerge,
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
  deriveChecksStatus,
  deriveMergeStatus,
  shouldShowPullRequestStats,
  useLinkIssuePullRequest,
  useSetIssuePRAutoComplete,
  useUnlinkIssuePullRequest,
  type PullRequestChecksStatus,
  type PullRequestMergeStatus,
} from "@multica/core/github";
import { useWorkspacePaths } from "@multica/core/paths";
import type {
  GitHubPullRequest,
  GitHubPullRequestState,
  PRAutoComplete,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import { AppLink } from "../../navigation";
import { useT, useTimeAgo } from "../../i18n";

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
        <PullRequestRow key={pr.id} pr={pr} actions={rowActions} />
      ))}
      {useCollapse ? (
        <div className="space-y-1">
          {expanded
            ? collapsedTail.map((pr) => <PullRequestRow key={pr.id} pr={pr} actions={rowActions} />)
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
        <AutoCompleteLine issueId={issueId} identifier={identifier} prs={prs} autoComplete={autoComplete} />
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

/**
 * Remove a PR from the issue. The server remembers the removal (webhooks will
 * not link it again) and treats it as a PR event, so removing the last
 * unmerged PR can complete the issue — the toast says so, and offers undo only
 * when nothing else changed.
 */
function useUnlinkPullRequest(issueId: string, before: PRAutoComplete) {
  const { t } = useT("issues");
  const unlink = useUnlinkIssuePullRequest(issueId);
  const relink = useLinkIssuePullRequest(issueId);
  return (pr: GitHubPullRequest) => {
    unlink.mutate(pr.id, {
      onSuccess: (after) => {
        const completed =
          before.state !== "terminal" && after.auto_complete?.state === "terminal";
        if (completed) {
          toast.success(t(($) => $.pr_automation.unlinked_completed, { pr: prLabel(pr) }));
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
            className="absolute top-1 right-0 opacity-0 group-hover/pr:opacity-100 group-focus-within/pr:opacity-100 data-popup-open:opacity-100 [@media(pointer:coarse)]:opacity-100"
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

/**
 * One line under the PR list saying what the "every linked PR merged, one says
 * Closes → Done" rule will do for this issue, straight from the server's
 * decision. Terminal, triage and unknown states render nothing.
 */
function AutoCompleteLine({
  issueId,
  identifier,
  prs,
  autoComplete,
}: {
  issueId: string;
  identifier: string;
  prs: GitHubPullRequest[];
  autoComplete: PRAutoComplete;
}) {
  const { t } = useT("issues");
  const paths = useWorkspacePaths();
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

  let icon: React.ReactNode;
  let body: React.ReactNode;
  switch (autoComplete.state) {
    case "waiting":
      if (named.length === 0) return null;
      icon = <CircleDashed className="text-muted-foreground" />;
      body = t(($) => $.pr_automation.waiting, { count: named.length, prs: list });
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
    case "no_close_intent":
      icon = <CircleSlash className="text-muted-foreground" />;
      body = t(($) => $.pr_automation.no_close_intent, { identifier });
      break;
    case "all_merged":
      icon = <CheckCircle2 className="text-muted-foreground" />;
      body = t(($) => $.pr_automation.all_merged);
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
    case "workspace_disabled":
      icon = <CircleSlash className="text-muted-foreground" />;
      body = (
        <>
          {t(($) => $.pr_automation.workspace_disabled)} ·{" "}
          <AppLink
            href={`${paths.settings()}?tab=issue-statuses`}
            className="font-medium text-foreground underline decoration-border underline-offset-2 hover:decoration-foreground"
          >
            {t(($) => $.pr_automation.workspace_disabled_action)}
          </AppLink>
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

function PullRequestRow({ pr, actions }: { pr: GitHubPullRequest; actions: RowActions | null }) {
  const { t } = useT("issues");
  const cfg = STATE_ICON[pr.state] ?? { icon: GitPullRequest, className: "" };
  const StateIcon = cfg.icon;
  const isDraft = pr.state === "draft";
  const stateLabel = getStateLabel(pr.state, t);

  // The row link and its menu are siblings: a button inside an anchor is
  // invalid HTML, and the menu must not open the PR.
  return (
    <div className="group/pr relative -mx-2 rounded-md transition-colors hover:bg-accent/50">
    <a
      data-testid="pull-request-row"
      href={pr.html_url}
      target="_blank"
      rel="noreferrer noopener"
      className={cn(
        "flex items-start gap-2 rounded-md px-2 py-1.5 group",
        actions ? "pr-7" : null,
        isDraft ? "opacity-80" : null,
      )}
    >
      <StateIcon className={cn("h-3.5 w-3.5 mt-0.5 shrink-0", cfg.className)} />
      <div className="min-w-0 flex-1">
        <p className="text-caption font-medium leading-snug truncate group-hover:text-foreground">
          {pr.title}
        </p>
        <p className="text-micro text-muted-foreground truncate">
          {pr.repo_owner}/{pr.repo_name}#{pr.number} · {stateLabel}
          {pr.author_login ? ` · @${pr.author_login}` : null}
        </p>
        <PullRequestRowDetails pr={pr} />
      </div>
    </a>
    {actions ? <PullRequestRowMenu pr={pr} actions={actions} /> : null}
    </div>
  );
}

function PullRequestRowDetails({ pr }: { pr: GitHubPullRequest }) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();

  const showStats = shouldShowPullRequestStats({
    additions: pr.additions,
    deletions: pr.deletions,
    changed_files: pr.changed_files,
  });

  // Neither status element is shown for terminal PRs — the leading state icon
  // already conveys merged / closed, and CI / mergeability are no longer
  // actionable there.
  const isTerminal = pr.state === "merged" || pr.state === "closed";
  const checksBadge = isTerminal ? null : getChecksBadge(deriveChecksStatus(pr), t);
  const mergeBadge = isTerminal ? null : getMergeBadge(deriveMergeStatus(pr), t);

  // A stale snapshot (GitHub outage / revoked key) greys out both elements and
  // annotates them with the snapshot age instead of hiding the last-known data.
  const stale = !isTerminal && pr.snapshot_stale === true;
  const staleTitle = stale
    ? pr.snapshot_fetched_at
      ? t(($) => $.detail.pull_request_snapshot_stale, { time: timeAgo(pr.snapshot_fetched_at) })
      : t(($) => $.detail.pull_request_snapshot_stale_unknown)
    : undefined;

  if (!showStats && !checksBadge && !mergeBadge) return null;

  return (
    <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-micro text-muted-foreground">
      {showStats ? <PullRequestStats pr={pr} /> : null}
      {checksBadge ? <PullRequestBadge badge={checksBadge} stale={stale} title={staleTitle} /> : null}
      {mergeBadge ? <PullRequestBadge badge={mergeBadge} stale={stale} title={staleTitle} /> : null}
    </div>
  );
}

function PullRequestStats({ pr }: { pr: GitHubPullRequest }) {
  const { t } = useT("issues");
  return (
    <span className="inline-flex items-center gap-1.5 tabular-nums">
      <span className="text-emerald-600 dark:text-emerald-400">+{pr.additions ?? 0}</span>
      <span className="text-rose-600 dark:text-rose-400">−{pr.deletions ?? 0}</span>
      <span aria-hidden="true">·</span>
      <span>
        {t(($) => $.detail.pull_request_card_files_count, {
          count: pr.changed_files ?? 0,
        })}
      </span>
    </span>
  );
}

interface PullRequestBadgeConfig {
  icon: React.ComponentType<{ className?: string }>;
  label: string;
  className: string;
}

function PullRequestBadge({
  badge,
  stale,
  title,
}: {
  badge: PullRequestBadgeConfig;
  stale?: boolean;
  title?: string;
}) {
  const Icon = badge.icon;
  return (
    <span
      className={cn("inline-flex items-center gap-1", stale ? "opacity-60" : null)}
      title={title}
    >
      <Icon className={cn("h-3 w-3", badge.className)} />
      {badge.label}
    </span>
  );
}

// CI element. A current snapshot with a null rollup renders "no checks yet";
// an unavailable/disabled snapshot renders nothing.
function getChecksBadge(
  status: PullRequestChecksStatus,
  t: IssuesT,
): PullRequestBadgeConfig | null {
  switch (status.kind) {
    case "failed":
      return {
        icon: XCircle,
        className: "text-rose-600 dark:text-rose-400",
        label: checksFailedLabel(status, t),
      };
    case "pending":
      return {
        icon: CircleDashed,
        className: "text-amber-600 dark:text-amber-400",
        label: t(($) => $.detail.pull_request_checks_running, {
          passed: status.passed,
          total: status.total,
          running: status.running,
        }),
      };
    case "passed":
      return {
        icon: CheckCircle2,
        className: "text-emerald-600 dark:text-emerald-400",
        label: t(($) => $.detail.pull_request_checks_all_passed, { total: status.total }),
      };
    case "none":
      return {
        icon: Circle,
        className: "text-muted-foreground",
        label: t(($) => $.detail.pull_request_checks_none),
      };
    case "unavailable":
      return null;
  }
}

function checksFailedLabel(
  status: Extract<PullRequestChecksStatus, { kind: "failed" }>,
  t: IssuesT,
): string {
  const shown = status.names.slice(0, 2);
  if (shown.length === 0) {
    return t(($) => $.detail.pull_request_checks_failed_count, {
      failed: status.failed,
      total: status.total,
    });
  }
  const remaining = status.names.length - shown.length;
  const parts = [...shown];
  if (remaining > 0) {
    parts.push(t(($) => $.detail.pull_request_checks_more, { count: remaining }));
  }
  return t(($) => $.detail.pull_request_checks_failed_named, {
    failed: status.failed,
    total: status.total,
    names: parts.join(", "),
  });
}

// Mergeability element. Returns null for the "none" state — when GitHub has not
// decided, the card asserts neither "conflict" nor "ready".
function getMergeBadge(status: PullRequestMergeStatus, t: IssuesT): PullRequestBadgeConfig | null {
  switch (status.kind) {
    case "conflicting":
      return {
        icon: TriangleAlert,
        className: "text-amber-600 dark:text-amber-400",
        label: t(($) => $.detail.pull_request_merge_conflicting),
      };
    case "ready":
      return {
        icon: CheckCircle2,
        className: "text-emerald-600 dark:text-emerald-400",
        label: t(($) => $.detail.pull_request_merge_ready),
      };
    case "blocked":
      return {
        icon: CircleSlash,
        className: "text-muted-foreground",
        label: t(($) => $.detail.pull_request_merge_blocked),
      };
    case "behind":
      return {
        icon: CircleSlash,
        className: "text-muted-foreground",
        label: t(($) => $.detail.pull_request_merge_behind),
      };
    case "unstable":
      return {
        icon: CircleSlash,
        className: "text-muted-foreground",
        label: t(($) => $.detail.pull_request_merge_unstable),
      };
    case "has_hooks":
      return {
        icon: CircleSlash,
        className: "text-muted-foreground",
        label: t(($) => $.detail.pull_request_merge_has_hooks),
      };
    case "none":
      return null;
  }
}

function getStateLabel(state: GitHubPullRequestState, t: IssuesT): string {
  return state === "open"
    ? t(($) => $.detail.pull_request_state_open)
    : state === "draft"
      ? t(($) => $.detail.pull_request_state_draft)
      : state === "merged"
        ? t(($) => $.detail.pull_request_state_merged)
        : state === "closed"
          ? t(($) => $.detail.pull_request_state_closed)
          : state;
}
