"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { issueGitLabIssueOptions } from "@multica/core/gitlab";
import { useLinkGitLabIssue, useUnlinkGitLabIssue } from "@multica/core/gitlab";
import { useT } from "../../i18n";

export function GitLabIssueBadge({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const { data, isLoading } = useQuery(issueGitLabIssueOptions(issueId));
  const [input, setInput] = useState("");
  const [linkError, setLinkError] = useState("");
  const link = useLinkGitLabIssue(issueId);
  const unlink = useUnlinkGitLabIssue(issueId);

  if (isLoading) return null;

  function handleLink(e: React.FormEvent) {
    e.preventDefault();
    setLinkError("");
    // Accept "group/project#123" or just "#123" or "123"
    const match = input.trim().match(/^(?:(.+?)#)?(\d+)$/);
    if (!match) {
      setLinkError(t(($) => $.gitlab_issue.link_error));
      return;
    }
    const projectPath = match[1] ?? "";
    const iid = parseInt(match[2] ?? "0", 10);
    if (!projectPath || !iid) {
      setLinkError(t(($) => $.gitlab_issue.link_error));
      return;
    }
    link.mutate(
      { projectPath, glIssueIid: iid },
      { onError: () => setLinkError(t(($) => $.gitlab_issue.link_error)) },
    );
  }

  if (data) {
    return (
      <div className="flex flex-col gap-1">
        <div className="flex items-center justify-between px-1.5">
          <p className="text-xs font-medium text-muted-foreground">
            {t(($) => $.gitlab_issue.title)}
          </p>
          <button
            onClick={() => unlink.mutate()}
            disabled={unlink.isPending}
            className="text-xs text-muted-foreground hover:text-foreground transition-colors"
          >
            {t(($) => $.gitlab_issue.unlink_button)}
          </button>
        </div>
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

  return (
    <div className="flex flex-col gap-1">
      <p className="px-1.5 text-xs font-medium text-muted-foreground">
        {t(($) => $.gitlab_issue.title)}
      </p>
      <form onSubmit={handleLink} className="flex gap-1 px-1.5">
        <input
          value={input}
          onChange={(e) => setInput(e.target.value)}
          placeholder={t(($) => $.gitlab_issue.link_placeholder)}
          className="flex-1 min-w-0 rounded-md border border-input bg-background px-2 py-1 text-xs focus:outline-none focus:ring-1 focus:ring-ring"
        />
        <button
          type="submit"
          disabled={link.isPending || !input.trim()}
          className="rounded-md bg-primary px-2 py-1 text-xs text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
        >
          {t(($) => $.gitlab_issue.link_button)}
        </button>
      </form>
      {linkError && (
        <p className="px-1.5 text-xs text-destructive">{linkError}</p>
      )}
    </div>
  );
}
