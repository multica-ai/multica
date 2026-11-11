"use client";

import { useQuery } from "@tanstack/react-query";
import {
  GitMerge,
  GitPullRequest,
  GitPullRequestArrow,
  GitPullRequestClosed,
  GitPullRequestDraft,
} from "@multica/ui/icons";
import { issuePullRequestSummariesOptions } from "@multica/core/github";
import { useWorkspaceId } from "@multica/core/hooks";
import type { GitHubPullRequestState, IssuePullRequestSummary } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

export const PULL_REQUEST_STATE_ICON: Record<
  GitHubPullRequestState,
  { icon: React.ComponentType<{ className?: string }>; className: string }
> = {
  open: { icon: GitPullRequestArrow, className: "text-emerald-600 dark:text-emerald-400" },
  draft: { icon: GitPullRequestDraft, className: "text-muted-foreground" },
  merged: { icon: GitMerge, className: "text-violet-600 dark:text-violet-400" },
  closed: { icon: GitPullRequestClosed, className: "text-rose-600 dark:text-rose-400" },
};

const EMPTY: readonly IssuePullRequestSummary[] = [];

/**
 * The issue's linked PRs, newest first, from the one workspace-wide summary
 * read. Disabled callers never start that request.
 */
export function useIssuePullRequestSummaries(
  issueId: string,
  enabled: boolean,
): readonly IssuePullRequestSummary[] {
  const wsId = useWorkspaceId();
  const { data } = useQuery({
    ...issuePullRequestSummariesOptions(wsId),
    enabled: enabled && !!wsId,
    select: (byIssue) => byIssue[issueId] ?? EMPTY,
  });
  return enabled ? (data ?? EMPTY) : EMPTY;
}

function isKnownState(state: string): state is GitHubPullRequestState {
  return state in PULL_REQUEST_STATE_ICON;
}

/**
 * Compact "state icon + #number" chips for issue cards, rows and table cells.
 * Board cards and list rows sit inside the issue link, so their chips stay
 * plain text; the table cell has no wrapping link and opens the PR instead.
 */
export function IssuePullRequestChips({
  pullRequests,
  max,
  linked = false,
  className,
}: {
  pullRequests: readonly IssuePullRequestSummary[];
  max: number;
  /** Render each chip as a link to the PR. Never inside another link. */
  linked?: boolean;
  className?: string;
}) {
  const { t } = useT("issues");
  if (pullRequests.length === 0) return null;
  const visible = pullRequests.slice(0, max);
  const hidden = pullRequests.length - visible.length;
  // The repo name only tells chips apart when the PRs span repos.
  const showRepo =
    new Set(pullRequests.map((pr) => `${pr.repo_owner}/${pr.repo_name}`)).size > 1;

  const stateLabel = (state: string) => {
    switch (state) {
      case "merged":
        return t(($) => $.detail.pull_request_state_merged);
      case "closed":
        return t(($) => $.detail.pull_request_state_closed);
      case "draft":
        return t(($) => $.detail.pull_request_state_draft);
      default:
        return t(($) => $.detail.pull_request_state_open);
    }
  };

  return (
    <span className={cn("inline-flex min-w-0 items-center gap-1", className)}>
      {visible.map((pr) => {
        const cfg = isKnownState(pr.state)
          ? PULL_REQUEST_STATE_ICON[pr.state]
          : { icon: GitPullRequest, className: "" };
        const Icon = cfg.icon;
        const label = stateLabel(pr.state);
        const chipClass =
          "inline-flex max-w-[140px] shrink-0 items-center gap-1 rounded-full bg-muted/60 px-1.5 py-0.5 text-micro text-muted-foreground tabular-nums";
        const content = (
          <>
            <Icon aria-hidden className={cn("size-3 shrink-0", cfg.className)} />
            <span className="sr-only">{label}</span>
            <span className="truncate">
              {showRepo ? pr.repo_name : null}#{pr.number}
            </span>
          </>
        );
        const title = `${pr.repo_owner}/${pr.repo_name}#${pr.number} · ${label}${pr.title ? `\n${pr.title}` : ""}`;
        return linked ? (
          <a
            key={pr.id}
            href={pr.html_url}
            target="_blank"
            rel="noreferrer noopener"
            title={title}
            data-testid="issue-pull-request-chip"
            className={cn(chipClass, "hover:bg-accent hover:text-foreground")}
            onClick={(event) => event.stopPropagation()}
            onAuxClick={(event) => event.stopPropagation()}
          >
            {content}
          </a>
        ) : (
          <span
            key={pr.id}
            title={title}
            data-testid="issue-pull-request-chip"
            className={chipClass}
          >
            {content}
          </span>
        );
      })}
      {hidden > 0 && (
        <span className="shrink-0 text-micro text-muted-foreground">+{hidden}</span>
      )}
    </span>
  );
}
