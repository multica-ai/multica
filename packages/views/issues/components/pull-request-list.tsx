"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  GitMerge,
  GitPullRequest,
  GitPullRequestArrow,
  GitPullRequestClosed,
  GitPullRequestDraft,
  TriangleAlert,
  XCircle,
} from "lucide-react";
import {
  issuePullRequestsOptions,
  derivePullRequestAlerts,
  shouldShowPullRequestStats,
} from "@multica/core/github";
import type { GitHubPullRequest, GitHubPullRequestState } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

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

export function PullRequestList({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const [expanded, setExpanded] = useState(false);
  const { data, isLoading } = useQuery(issuePullRequestsOptions(issueId));
  const prs = data?.pull_requests ?? [];

  if (isLoading) {
    return <p className="text-xs text-muted-foreground px-2">{t(($) => $.detail.pull_requests_loading)}</p>;
  }
  if (prs.length === 0) {
    return (
      <p className="text-xs text-muted-foreground px-2">
        {t(($) => $.detail.pull_requests_empty)}
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
        <PullRequestRow key={pr.id} pr={pr} />
      ))}
      {useCollapse ? (
        <div className="space-y-1">
          {expanded
            ? collapsedTail.map((pr) => <PullRequestRow key={pr.id} pr={pr} />)
            : null}
          <button
            type="button"
            onClick={() => setExpanded((v) => !v)}
            className="block w-[calc(100%+1rem)] -mx-2 rounded-md px-2 py-1.5 text-left text-[11px] text-muted-foreground hover:bg-accent/50 hover:text-foreground transition-colors"
          >
            {expanded
              ? t(($) => $.detail.pull_request_card_show_less)
              : t(($) => $.detail.pull_request_card_show_more, { count: collapsedTail.length })}
          </button>
        </div>
      ) : null}
    </div>
  );
}

function PullRequestRow({ pr }: { pr: GitHubPullRequest }) {
  const { t } = useT("issues");
  const cfg = STATE_ICON[pr.state] ?? { icon: GitPullRequest, className: "" };
  const StateIcon = cfg.icon;
  const stateLabel = getStateLabel(pr.state, t);

  return (
    <a
      data-testid="pull-request-row"
      href={pr.html_url}
      target="_blank"
      rel="noreferrer noopener"
      className={cn(
        "flex items-start gap-2 rounded-md px-2 py-1.5 -mx-2 hover:bg-accent/50 transition-colors group",
        pr.state === "draft" ? "opacity-80" : null,
      )}
    >
      <StateIcon className={cn("h-3.5 w-3.5 mt-0.5 shrink-0", cfg.className)} />
      <div className="min-w-0 flex-1">
        <p className="text-xs font-medium leading-snug truncate group-hover:text-foreground">
          {pr.title}
        </p>
        <p className="text-[11px] text-muted-foreground truncate">
          {pr.repo_owner}/{pr.repo_name}#{pr.number} · {stateLabel}
          {pr.author_login ? ` · @${pr.author_login}` : null}
        </p>
        <PullRequestRowDetails pr={pr} />
      </div>
    </a>
  );
}

// Fail-only detail line (MUL-5180): diff stats plus at most two alerts. The
// row is an alarm, not a dashboard — passed / pending / ready states render
// nothing because completed-suite webhooks cannot vouch for them (a green
// roll-up may just mean the slower provider has not reported yet). Failure
// and conflicts are the two signals GitHub delivers reliably, so they are
// the only ones shown; both can be true at once and must not mask each other.
function PullRequestRowDetails({ pr }: { pr: GitHubPullRequest }) {
  const { t } = useT("issues");
  const alerts = derivePullRequestAlerts({
    state: pr.state,
    mergeable_state: pr.mergeable_state,
    checks_failed: pr.checks_failed,
  });
  const showStats = shouldShowPullRequestStats({
    additions: pr.additions,
    deletions: pr.deletions,
    changed_files: pr.changed_files,
  });
  if (!showStats && !alerts.checksFailed && !alerts.conflicts) return null;

  return (
    <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[11px] text-muted-foreground">
      {showStats ? <PullRequestStats pr={pr} /> : null}
      {alerts.checksFailed ? (
        <PullRequestAlertText
          kind="checks_failed"
          icon={XCircle}
          label={t(($) => $.detail.pull_request_checks_failed)}
        />
      ) : null}
      {alerts.conflicts ? (
        <PullRequestAlertText
          kind="conflicts"
          icon={TriangleAlert}
          label={t(($) => $.detail.pull_request_card_status_conflicts)}
        />
      ) : null}
    </div>
  );
}

function PullRequestAlertText({
  kind,
  icon: Icon,
  label,
}: {
  kind: "checks_failed" | "conflicts";
  icon: React.ComponentType<{ className?: string }>;
  label: string;
}) {
  return (
    <span
      data-testid="pull-request-alert"
      data-alert-kind={kind}
      className="inline-flex min-w-0 items-center gap-1 font-medium text-rose-600 dark:text-rose-400"
    >
      <Icon className="h-3 w-3 shrink-0" />
      <span className="truncate">{label}</span>
    </span>
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

function getStateLabel(
  state: GitHubPullRequestState,
  t: IssuesT,
): string {
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
