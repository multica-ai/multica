import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { issueTimelineOptions } from "@multica/core/issues/queries";
import { issuePullRequestsOptions } from "@multica/core/github/queries";
import type { AgentTask } from "@multica/core/types/agent";
import { matchRunComments, matchRunPullRequests } from "./run-deliverables";

/**
 * What the run delivered outside its working copy: the comments it posted and
 * the pull requests opened while it ran. Both queries are the issue page's
 * own, so opening the overview from an issue normally costs no request.
 */
export function useRunDeliverables(task: AgentTask, enabled: boolean) {
  const issueId = task.issue_id;
  const active = enabled && issueId.length > 0;
  const { data: timeline } = useQuery({ ...issueTimelineOptions(issueId), enabled: active });
  const { data: pullRequests } = useQuery({ ...issuePullRequestsOptions(issueId), enabled: active });

  const comments = useMemo(() => matchRunComments(timeline, task.id), [timeline, task.id]);
  const prs = useMemo(
    () => matchRunPullRequests(pullRequests?.pull_requests, task),
    [pullRequests, task],
  );
  return { comments, pullRequests: prs };
}
