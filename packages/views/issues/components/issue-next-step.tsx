"use client";

import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { issuePullRequestsOptions, useGitHubSettings } from "@multica/core/github";
import { GitPullRequest, MessageSquare } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";

/** The comment a blocked issue is waiting on, when an agent wrote the latest one. */
export interface IssueNextStepReply {
  authorName: string;
  onReply: () => void;
}

/**
 * The overview card's "what does this need from me" line. Built-in rules only,
 * each backed by data the page already has: an issue in review with an open
 * PR asks for a review; a blocked issue whose latest comment came from an
 * agent asks for a reply. Anything else renders nothing.
 */
export function IssueNextStep({
  issueId,
  status,
  reply,
}: {
  issueId: string;
  status: string;
  reply: IssueNextStepReply | null;
}) {
  const { t } = useT("issues");
  // Off with the workspace's GitHub integration or its PR sidebar, the same
  // switch that hides the Pull requests section.
  const { prSidebar } = useGitHubSettings();
  const asksReview = status === "in_review" && prSidebar;
  const { data } = useQuery({ ...issuePullRequestsOptions(issueId), enabled: asksReview });
  const openPr = asksReview ? data?.pull_requests.find((pr) => pr.state === "open") : undefined;

  if (openPr) {
    return (
      <NextStep
        icon={<GitPullRequest className="size-3.5 text-success" />}
        title={t(($) => $.detail.next_step.review_title)}
        detail={`#${openPr.number} ${openPr.title}`}
        action={
          <Button size="xs" render={<a href={openPr.html_url} target="_blank" rel="noreferrer noopener" />}>
            {t(($) => $.detail.next_step.review_action)}
          </Button>
        }
      />
    );
  }
  if (status === "blocked" && reply) {
    return (
      <NextStep
        icon={<MessageSquare className="size-3.5 text-muted-foreground" />}
        title={t(($) => $.detail.next_step.reply_title, { name: reply.authorName })}
        action={
          <Button size="xs" variant="outline" onClick={reply.onReply}>
            {t(($) => $.detail.next_step.reply_action)}
          </Button>
        }
      />
    );
  }
  return null;
}

function NextStep({ icon, title, detail, action }: { icon: ReactNode; title: string; detail?: string; action: ReactNode }) {
  return (
    <div data-slot="issue-next-step" className="flex flex-col gap-2 px-4 pt-1 pb-1.5">
      <div className="flex min-w-0 items-start gap-2">
        <span className="flex h-5 shrink-0 items-center">{icon}</span>
        <div className="min-w-0 flex-1">
          <p className="text-label font-medium">{title}</p>
          {detail && <p className="truncate text-caption text-muted-foreground" title={detail}>{detail}</p>}
        </div>
      </div>
      <div className="pl-5.5">{action}</div>
    </div>
  );
}
