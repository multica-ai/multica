"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  GitMerge,
  GitPullRequest,
  GitPullRequestClosed,
  Lock,
} from "lucide-react";
import { issueMergeRequestsOptions } from "@multica/core/gitlab";
import type { GitLabMergeRequest, GitLabMRState } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

const MR_LIMIT_BEFORE_COLLAPSE = 4;

const STATE_ICON: Record<
  GitLabMRState,
  { icon: React.ComponentType<{ className?: string }>; className: string }
> = {
  open:   { icon: GitPullRequest,       className: "text-emerald-600 dark:text-emerald-400" },
  merged: { icon: GitMerge,             className: "text-violet-600 dark:text-violet-400" },
  closed: { icon: GitPullRequestClosed, className: "text-rose-600 dark:text-rose-400" },
  locked: { icon: Lock,                 className: "text-muted-foreground" },
};

function MergeRequestRow({ mr }: { mr: GitLabMergeRequest }) {
  const StateIcon = STATE_ICON[mr.state] ?? STATE_ICON.open;
  return (
    <a
      href={mr.html_url}
      target="_blank"
      rel="noopener noreferrer"
      className="flex items-start gap-2 rounded-md p-1.5 text-sm hover:bg-muted/50 transition-colors"
    >
      <StateIcon.icon className={cn("mt-0.5 h-4 w-4 shrink-0", StateIcon.className)} />
      <span className="min-w-0 flex-1 truncate text-foreground">{mr.title}</span>
    </a>
  );
}

export function MergeRequestList({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const [expanded, setExpanded] = useState(false);
  const { data, isLoading } = useQuery(issueMergeRequestsOptions(issueId));
  const mrs = data?.merge_requests ?? [];

  if (isLoading || mrs.length === 0) return null;

  const visible = expanded ? mrs : mrs.slice(0, MR_LIMIT_BEFORE_COLLAPSE);
  const hiddenCount = mrs.length - MR_LIMIT_BEFORE_COLLAPSE;

  return (
    <div className="flex flex-col gap-1">
      <p className="px-1.5 text-xs font-medium text-muted-foreground">
        {t(($) => $.merge_requests.title)}
      </p>
      {visible.map((mr) => (
        <MergeRequestRow key={mr.id} mr={mr} />
      ))}
      {mrs.length > MR_LIMIT_BEFORE_COLLAPSE && (
        <button
          type="button"
          className="px-1.5 text-left text-xs text-muted-foreground hover:text-foreground"
          onClick={() => setExpanded((v) => !v)}
        >
          {expanded
            ? t(($) => $.merge_requests.show_less)
            : t(($) => $.merge_requests.show_more, { count: hiddenCount })}
        </button>
      )}
    </div>
  );
}
