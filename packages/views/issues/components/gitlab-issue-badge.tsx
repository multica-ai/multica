"use client";

import { useQuery } from "@tanstack/react-query";
import { issueGitLabIssueOptions } from "@multica/core/gitlab";
import { useT } from "../../i18n";

export function GitLabIssueBadge({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const { data, isLoading } = useQuery(issueGitLabIssueOptions(issueId));

  if (isLoading || !data) return null;

  return (
    <div className="flex flex-col gap-1">
      <p className="px-1.5 text-xs font-medium text-muted-foreground">
        {t(($) => $.gitlab_issue.title)}
      </p>
      <a
        href={data.url}
        target="_blank"
        rel="noopener noreferrer"
        className="flex items-center gap-1.5 rounded-md px-1.5 py-1 text-sm hover:bg-muted/50 transition-colors text-foreground"
      >
        <span className="truncate">{data.project_path}#{data.gl_issue_iid}</span>
      </a>
      {data.gl_assignee_username && (
        <p className="px-1.5 text-xs text-muted-foreground">
          {t(($) => $.gitlab_issue.assignee_label)}: {data.gl_assignee_username}
        </p>
      )}
    </div>
  );
}
