"use client";

import {
  GitMerge,
  GitPullRequest,
  GitPullRequestArrow,
  GitPullRequestClosed,
  GitPullRequestDraft,
} from "lucide-react";
import { deriveChecksStatus } from "@multica/core/github";
import type { IssueLinkedPullRequest } from "@multica/core/types";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { useT } from "../../i18n";

const stateConfig = {
  open: { icon: GitPullRequestArrow, color: "text-emerald-600 dark:text-emerald-400" },
  draft: { icon: GitPullRequestDraft, color: "text-muted-foreground" },
  merged: { icon: GitMerge, color: "text-violet-600 dark:text-violet-400" },
  closed: { icon: GitPullRequestClosed, color: "text-rose-600 dark:text-rose-400" },
};

function PRMark({ pr }: { pr: IssueLinkedPullRequest }) {
  const config = stateConfig[pr.state as keyof typeof stateConfig];
  const Icon = config?.icon ?? GitPullRequest;
  const checks = deriveChecksStatus(pr);
  const ciColor = checks.kind === "failed"
    ? "bg-rose-500"
    : checks.kind === "pending"
      ? "bg-amber-500"
      : checks.kind === "passed"
        ? "bg-emerald-500"
        : null;
  return (
    <span className="relative inline-flex size-4 shrink-0 items-center justify-center">
      <Icon className={`size-3.5 ${config?.color ?? "text-muted-foreground"}`} />
      {ciColor && (pr.state === "open" || pr.state === "draft") ? (
        <span
          data-testid={`linked-pr-ci-${checks.kind}`}
          className={`absolute -bottom-0.5 -right-0.5 size-1.5 rounded-full ring-1 ring-background ${ciColor}`}
        />
      ) : null}
    </span>
  );
}

/** Small PR link for list rows, board cards, and the detail header. */
export function LinkedPRIndicator({ prs }: { prs: IssueLinkedPullRequest[] | undefined }) {
  const { t } = useT("issues");
  const first = prs?.[0];
  if (!prs || !first) return null;

  const stateLabel = (state: string) => {
    switch (state) {
      case "open": return t(($) => $.detail.pull_request_state_open);
      case "draft": return t(($) => $.detail.pull_request_state_draft);
      case "merged": return t(($) => $.detail.pull_request_state_merged);
      case "closed": return t(($) => $.detail.pull_request_state_closed);
      default: return state;
    }
  };

  if (prs.length === 1) {
    const pr = first;
    return (
      <a
        href={pr.html_url}
        target="_blank"
        rel="noreferrer noopener"
        title={`${t(($) => $.detail.section_pull_requests)} #${pr.number} · ${stateLabel(pr.state)}`}
        aria-label={`${t(($) => $.detail.section_pull_requests)} #${pr.number} · ${stateLabel(pr.state)}`}
        className="inline-flex shrink-0 items-center gap-0.5 rounded-xs p-0.5 hover:bg-accent focus-visible:outline-ring"
        onClick={(event) => event.stopPropagation()}
      >
        <PRMark pr={pr} />
      </a>
    );
  }

  return (
    <Popover>
      <PopoverTrigger
        render={
          <button
            type="button"
            aria-label={`${t(($) => $.detail.section_pull_requests)} (${prs.length})`}
            className="inline-flex shrink-0 items-center gap-0.5 rounded-xs px-0.5 text-micro text-muted-foreground hover:bg-accent focus-visible:outline-ring"
            onClick={(event) => event.stopPropagation()}
          >
            <PRMark pr={first} />
            <span className="tabular-nums">{prs.length}</span>
          </button>
        }
      />
      <PopoverContent align="end" className="w-72 p-1.5">
        {prs.map((pr) => (
          <a
            key={`${pr.provider}-${pr.html_url}`}
            href={pr.html_url}
            target="_blank"
            rel="noreferrer noopener"
            className="flex items-center gap-2 rounded-md px-2 py-1.5 hover:bg-accent"
            onClick={(event) => event.stopPropagation()}
          >
            <PRMark pr={pr} />
            <span className="min-w-0 flex-1 truncate text-caption">{pr.title}</span>
            <span className="text-micro text-muted-foreground">#{pr.number} · {stateLabel(pr.state)}</span>
          </a>
        ))}
      </PopoverContent>
    </Popover>
  );
}
