"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  CheckCircle2,
  CircleDashed,
  GitMerge,
  GitPullRequest,
  GitPullRequestArrow,
  GitPullRequestClosed,
  GitPullRequestDraft,
  ShieldCheck,
  ShieldX,
  XCircle,
} from "lucide-react";
import { issueADOPullRequestsOptions } from "@multica/core/azuredevops";
import type { ADOPullRequest, ADOPullRequestState, ADOChecksConclusion, ADOPolicyStatus } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

const PR_LIMIT_BEFORE_COLLAPSE = 4;

const STATE_ICON: Record<
  ADOPullRequestState,
  { icon: React.ComponentType<{ className?: string }>; className: string }
> = {
  open: { icon: GitPullRequestArrow, className: "text-emerald-600 dark:text-emerald-400" },
  draft: { icon: GitPullRequestDraft, className: "text-muted-foreground" },
  merged: { icon: GitMerge, className: "text-violet-600 dark:text-violet-400" },
  abandoned: { icon: GitPullRequestClosed, className: "text-rose-600 dark:text-rose-400" },
};

const CHECKS_ICON: Record<
  ADOChecksConclusion,
  { icon: React.ComponentType<{ className?: string }>; className: string }
> = {
  passed: { icon: CheckCircle2, className: "text-emerald-600 dark:text-emerald-400" },
  failed: { icon: XCircle, className: "text-rose-600 dark:text-rose-400" },
  pending: { icon: CircleDashed, className: "text-amber-600 dark:text-amber-400" },
};

const POLICY_ICON: Record<
  ADOPolicyStatus,
  { icon: React.ComponentType<{ className?: string }>; className: string }
> = {
  approved: { icon: ShieldCheck, className: "text-emerald-600 dark:text-emerald-400" },
  blocked: { icon: ShieldX, className: "text-rose-600 dark:text-rose-400" },
  pending: { icon: CircleDashed, className: "text-amber-600 dark:text-amber-400" },
};

export function ADOPullRequestList({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const [expanded, setExpanded] = useState(false);
  const { data, isLoading } = useQuery(issueADOPullRequestsOptions(issueId));
  const prs = data?.pull_requests ?? [];

  if (isLoading) {
    return <p className="text-xs text-muted-foreground px-2">{t(($) => $.detail.ado_pull_requests_loading)}</p>;
  }
  if (prs.length === 0) {
    return (
      <p className="text-xs text-muted-foreground px-2">
        {t(($) => $.detail.ado_pull_requests_empty)}
      </p>
    );
  }

  const useCollapse = prs.length >= PR_LIMIT_BEFORE_COLLAPSE;
  const expandedHead = useCollapse ? prs.slice(0, PR_LIMIT_BEFORE_COLLAPSE - 1) : prs;
  const collapsedTail = useCollapse ? prs.slice(PR_LIMIT_BEFORE_COLLAPSE - 1) : [];

  return (
    <div className="space-y-1">
      {expandedHead.map((pr) => (
        <ADOPullRequestRow key={pr.id} pr={pr} />
      ))}
      {useCollapse ? (
        <div className="space-y-1">
          {expanded
            ? collapsedTail.map((pr) => <ADOPullRequestRow key={pr.id} pr={pr} />)
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

function ADOPullRequestRow({ pr }: { pr: ADOPullRequest }) {
  const { t } = useT("issues");
  const cfg = STATE_ICON[pr.state] ?? { icon: GitPullRequest, className: "" };
  const StateIcon = cfg.icon;
  const isDraft = pr.state === "draft";
  const stateLabel = getStateLabel(pr.state, t);

  const total = (pr.checks_passed ?? 0) + (pr.checks_failed ?? 0) + (pr.checks_pending ?? 0);
  const segments: { kind: "passed" | "failed" | "pending"; ratio: number }[] =
    total > 0
      ? [
          { kind: "passed" as const, ratio: (pr.checks_passed ?? 0) / total },
          { kind: "failed" as const, ratio: (pr.checks_failed ?? 0) / total },
          { kind: "pending" as const, ratio: (pr.checks_pending ?? 0) / total },
        ].filter((s) => s.ratio > 0)
      : [];

  return (
    <a
      data-testid="ado-pull-request-row"
      href={pr.html_url}
      target="_blank"
      rel="noreferrer noopener"
      className={cn(
        "flex items-start gap-2 rounded-md px-2 py-1.5 -mx-2 hover:bg-accent/50 transition-colors group",
        isDraft ? "opacity-80" : null,
      )}
    >
      <StateIcon className={cn("h-3.5 w-3.5 mt-0.5 shrink-0", cfg.className)} />
      <div className="min-w-0 flex-1">
        <p className="text-xs font-medium leading-snug truncate group-hover:text-foreground">
          {pr.title}
        </p>
        <p className="text-[11px] text-muted-foreground truncate">
          {pr.project}/{pr.repo_name}!{pr.number} · {stateLabel}
          {pr.author_login ? ` · @${pr.author_login}` : null}
        </p>
        <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[11px] text-muted-foreground">
          {segments.length > 0 && (
            <span className="flex h-1 w-12 shrink-0 overflow-hidden rounded-full bg-muted" aria-hidden="true">
              {segments.map((seg) => (
                <span
                  key={seg.kind}
                  className={cn(
                    "h-full block",
                    seg.kind === "failed" && "bg-rose-500 dark:bg-rose-400",
                    seg.kind === "pending" && "bg-amber-500 dark:bg-amber-400",
                    seg.kind === "passed" && "bg-emerald-500 dark:bg-emerald-400",
                  )}
                  style={{ width: `${seg.ratio * 100}%` }}
                />
              ))}
            </span>
          )}
          {pr.checks_conclusion && CHECKS_ICON[pr.checks_conclusion] && (
            <ChecksBadge conclusion={pr.checks_conclusion} t={t} />
          )}
          {pr.policy_status && <PolicyBadge status={pr.policy_status} t={t} />}
        </div>
      </div>
    </a>
  );
}

type IssuesT = ReturnType<typeof useT<"issues">>["t"];

function ChecksBadge({ conclusion, t }: { conclusion: ADOChecksConclusion; t: IssuesT }) {
  const cfg = CHECKS_ICON[conclusion];
  const Icon = cfg.icon;
  const label =
    conclusion === "passed"
      ? t(($) => $.detail.pull_request_checks_passed)
      : conclusion === "failed"
        ? t(($) => $.detail.pull_request_checks_failed)
        : t(($) => $.detail.pull_request_checks_pending);
  return (
    <span className="inline-flex items-center gap-1">
      <Icon className={cn("h-3 w-3", cfg.className)} />
      {label}
    </span>
  );
}

function PolicyBadge({ status, t }: { status: ADOPolicyStatus; t: IssuesT }) {
  const cfg = POLICY_ICON[status];
  const Icon = cfg.icon;
  const label =
    status === "approved"
      ? t(($) => $.detail.ado_policy_approved)
      : status === "blocked"
        ? t(($) => $.detail.ado_policy_blocked)
        : t(($) => $.detail.ado_policy_pending);
  return (
    <span className="inline-flex items-center gap-1">
      <Icon className={cn("h-3 w-3", cfg.className)} />
      {label}
    </span>
  );
}

function getStateLabel(state: ADOPullRequestState, t: IssuesT): string {
  switch (state) {
    case "open":
      return t(($) => $.detail.pull_request_state_open);
    case "draft":
      return t(($) => $.detail.pull_request_state_draft);
    case "merged":
      return t(($) => $.detail.pull_request_state_merged);
    case "abandoned":
      return t(($) => $.detail.ado_pr_state_abandoned);
  }
}
