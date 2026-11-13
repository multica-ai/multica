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
import { openExternal } from "../../platform/open-external";

export const PULL_REQUEST_STATE_ICON: Record<
  GitHubPullRequestState,
  { icon: React.ComponentType<{ className?: string }>; className: string }
> = {
  open: { icon: GitPullRequestArrow, className: "text-pr-open dark:text-pr-open-dark" },
  draft: { icon: GitPullRequestDraft, className: "text-muted-foreground" },
  merged: { icon: GitMerge, className: "text-pr-merged dark:text-pr-merged-dark" },
  closed: { icon: GitPullRequestClosed, className: "text-pr-closed dark:text-pr-closed-dark" },
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

const stopPress = (e: React.SyntheticEvent) => e.stopPropagation();

/**
 * A chip inside the issue link. An anchor cannot nest in an anchor, so this
 * is a `role="link"` span that opens the PR itself, the way
 * IssueDuplicateOfMarker's in-row control does. The PR is always external, so
 * click, modifier click, middle click and Enter all open it outside the app
 * and none of them reach the row: not its navigation, peek or drag.
 */
function NestedChipLink({
  href,
  title,
  className,
  children,
}: {
  href: string;
  title: string;
  className: string;
  children: React.ReactNode;
}) {
  const open = (e: React.SyntheticEvent) => {
    e.preventDefault();
    e.stopPropagation();
    openExternal(href);
  };
  return (
    <span
      role="link"
      tabIndex={0}
      title={title}
      data-testid="issue-pull-request-chip"
      className={className}
      onMouseDown={stopPress}
      onPointerDown={stopPress}
      onClick={open}
      onAuxClick={(e) => {
        if (e.button === 1) open(e);
      }}
      onKeyDown={(e) => {
        if (e.key === "Enter") open(e);
      }}
    >
      {children}
    </span>
  );
}

/**
 * Compact "state icon + #number" chips for issue cards, rows and table cells.
 * Every chip opens its PR: a plain anchor where nothing wraps it (table
 * cells), a NestedChipLink inside the issue link of board cards and list rows.
 *
 * The group fits whatever width its slot gives it: the newest PRs that fit at
 * their natural width are shown and "+N" counts every PR that is not. At
 * least one chip always shows; squeezed further, its repo name truncates
 * while the number stays whole.
 */
export function IssuePullRequestChips({
  pullRequests,
  max,
  insideLink = false,
  className,
}: {
  pullRequests: readonly IssuePullRequestSummary[];
  /** Most chips to show even when more would fit. */
  max: number;
  /** The chips sit inside the issue's own link (board cards, list rows). */
  insideLink?: boolean;
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
  const linkClass = cn(
    chipClass,
    // Inset: the chip row clips overflow, which would cut an outer ring.
    "cursor-pointer transition-colors hover:bg-accent hover:text-foreground focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring",
  );
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
          return insideLink ? (
            <NestedChipLink key={pr.id} href={pr.html_url} title={title} className={linkClass}>
              {chipContent(pr, label)}
            </NestedChipLink>
          ) : (
            <a
              key={pr.id}
              href={pr.html_url}
              target="_blank"
              rel="noreferrer noopener"
              title={title}
              data-testid="issue-pull-request-chip"
              className={linkClass}
              onClick={(event) => event.stopPropagation()}
              onAuxClick={(event) => event.stopPropagation()}
            >
              {chipContent(pr, label)}
            </a>
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
