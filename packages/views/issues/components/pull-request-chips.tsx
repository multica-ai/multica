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
import { useSingleRowFit } from "../../common/single-row-fit";
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

const CHIP_GAP = 4;
// Room for "+N" (up to "+99" at text-micro) beside the chips that fit.
const OVERFLOW_RESERVE = 24;

/**
 * Compact "state icon + #number" chips for issue cards, rows and table cells.
 * Board cards and list rows sit inside the issue link, so their chips stay
 * plain text; the table cell has no wrapping link and opens the PR instead.
 *
 * The group fits whatever width its slot gives it: the newest PRs that fit at
 * their natural width are shown and "+N" counts every PR that is not. At
 * least one chip always shows; squeezed further, its repo name truncates
 * while the number stays whole.
 */
export function IssuePullRequestChips({
  pullRequests,
  max,
  linked = false,
  className,
}: {
  pullRequests: readonly IssuePullRequestSummary[];
  /** Most chips to show even when more would fit. */
  max: number;
  /** Render each chip as a link to the PR. Never inside another link. */
  linked?: boolean;
  className?: string;
}) {
  const { t } = useT("issues");
  const candidates = pullRequests.slice(0, max);
  const { containerRef, measureRef, fitCount } = useSingleRowFit<HTMLSpanElement>({
    count: candidates.length,
    gap: CHIP_GAP,
    reserve: OVERFLOW_RESERVE,
    // Past `max`, "+N" always shows, so its room is always held back.
    reserveOnOverflow: candidates.length === pullRequests.length,
  });
  if (pullRequests.length === 0) return null;
  const visible = candidates.slice(0, Math.max(1, fitCount));
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

  const chipClass =
    "inline-flex min-w-0 max-w-[140px] items-center gap-1 rounded-full bg-muted/60 px-1.5 py-0.5 text-micro text-muted-foreground tabular-nums";
  const chipContent = (pr: IssuePullRequestSummary, label: string) => {
    const cfg = isKnownState(pr.state)
      ? PULL_REQUEST_STATE_ICON[pr.state]
      : { icon: GitPullRequest, className: "" };
    const Icon = cfg.icon;
    return (
      <>
        <Icon aria-hidden className={cn("size-3 shrink-0", cfg.className)} />
        <span className="sr-only">{label}</span>
        {/* "repo#n" reads as one token; squeezed, the repo name truncates. */}
        <span className="flex min-w-0">
          {showRepo ? <span className="min-w-0 truncate">{pr.repo_name}</span> : null}
          <span data-testid="issue-pull-request-number" className="shrink-0">#{pr.number}</span>
        </span>
      </>
    );
  };

  // The grid sizes the group from the mirror's natural width (capped by the
  // slot), never from the chips currently shown — otherwise a group that
  // dropped a chip could never grow wide enough to bring it back.
  return (
    <span
      data-testid="issue-pull-request-chips"
      className={cn("inline-grid min-w-0 max-w-full", className)}
    >
      <span
        ref={containerRef}
        className="flex min-w-0 items-center gap-1 overflow-hidden"
      >
        {visible.map((pr) => {
          const label = stateLabel(pr.state);
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
              {chipContent(pr, label)}
            </a>
          ) : (
            <span
              key={pr.id}
              title={title}
              data-testid="issue-pull-request-chip"
              className={chipClass}
            >
              {chipContent(pr, label)}
            </span>
          );
        })}
        {hidden > 0 && (
          <span
            data-testid="issue-pull-request-overflow"
            className="shrink-0 text-micro text-muted-foreground tabular-nums"
          >
            +{hidden}
          </span>
        )}
      </span>
      {/* Measurement mirror: every candidate at natural width, no height. */}
      <span
        ref={measureRef}
        aria-hidden
        className={cn(
          "pointer-events-none invisible flex h-0 gap-1 overflow-hidden",
          candidates.length < pullRequests.length && "pr-7",
        )}
      >
        {candidates.map((pr) => (
          <span key={pr.id} className={cn(chipClass, "shrink-0")}>
            {chipContent(pr, "")}
          </span>
        ))}
      </span>
    </span>
  );
}
