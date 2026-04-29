"use client";

import { useQuery } from "@tanstack/react-query";
import {
  GitPullRequest,
  GitPullRequestArrow,
  GitPullRequestClosed,
  GitMerge,
  GitPullRequestDraft,
} from "lucide-react";
import { issuePullRequestsOptions } from "@multica/core/github/queries";
import type { GitHubPullRequest, GitHubPullRequestState } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";

const STATE_CONFIG: Record<
  GitHubPullRequestState,
  { label: string; icon: React.ComponentType<{ className?: string }>; className: string }
> = {
  open: { label: "Open", icon: GitPullRequestArrow, className: "text-emerald-600 dark:text-emerald-400" },
  draft: { label: "Draft", icon: GitPullRequestDraft, className: "text-muted-foreground" },
  merged: { label: "Merged", icon: GitMerge, className: "text-violet-600 dark:text-violet-400" },
  closed: { label: "Closed", icon: GitPullRequestClosed, className: "text-rose-600 dark:text-rose-400" },
};

export function PullRequestList({ issueId }: { issueId: string }) {
  const { data, isLoading } = useQuery(issuePullRequestsOptions(issueId));
  const prs = data?.pull_requests ?? [];

  if (isLoading) {
    return <p className="text-xs text-muted-foreground px-2">Loading…</p>;
  }
  if (prs.length === 0) {
    return (
      <p className="text-xs text-muted-foreground px-2">
        No linked pull requests yet. Reference{" "}
        <span className="font-medium">this issue&apos;s identifier</span> in a PR&apos;s branch name,
        title, or body to auto-link it.
      </p>
    );
  }

  return (
    <div className="space-y-1">
      {prs.map((pr) => (
        <PullRequestRow key={pr.id} pr={pr} />
      ))}
    </div>
  );
}

function PullRequestRow({ pr }: { pr: GitHubPullRequest }) {
  const cfg = STATE_CONFIG[pr.state] ?? { label: pr.state, icon: GitPullRequest, className: "" };
  const Icon = cfg.icon;
  return (
    <a
      href={pr.html_url}
      target="_blank"
      rel="noreferrer noopener"
      className="flex items-start gap-2 rounded-md px-2 py-1.5 -mx-2 hover:bg-accent/50 transition-colors group"
    >
      <Icon className={cn("h-3.5 w-3.5 mt-0.5 shrink-0", cfg.className)} />
      <div className="min-w-0 flex-1">
        <p className="text-xs font-medium truncate group-hover:text-foreground">{pr.title}</p>
        <p className="text-[11px] text-muted-foreground truncate">
          {pr.repo_owner}/{pr.repo_name}#{pr.number} · {cfg.label}
          {pr.author_login ? ` · @${pr.author_login}` : null}
        </p>
      </div>
    </a>
  );
}
