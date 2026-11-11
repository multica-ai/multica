// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { TimelineEntry } from "@multica/core/types/activity";
import type { AgentTask } from "@multica/core/types/agent";
import type { GitHubPullRequest } from "@multica/core/types/github";
import { matchRunComments, matchRunPullRequests } from "./run-deliverables";

function comment(id: string, sourceTaskId: string | null): TimelineEntry {
  return {
    type: "comment",
    id,
    actor_type: "agent",
    actor_id: "agent-1",
    created_at: "2026-06-08T08:05:00Z",
    content: `comment ${id}`,
    source_task_id: sourceTaskId,
  };
}

function pr(id: string, branch: string | null, createdAt: string): GitHubPullRequest {
  return {
    id,
    workspace_id: "workspace-1",
    repo_owner: "acme",
    repo_name: "app",
    number: 1,
    title: id,
    state: "open",
    html_url: `https://github.com/acme/app/pull/${id}`,
    branch,
    author_login: null,
    author_avatar_url: null,
    merged_at: null,
    closed_at: null,
    pr_created_at: createdAt,
    pr_updated_at: createdAt,
  };
}

const run: Pick<AgentTask, "branch_name" | "dispatched_at" | "started_at" | "completed_at"> = {
  dispatched_at: "2026-06-08T07:59:00Z",
  started_at: "2026-06-08T08:00:00Z",
  completed_at: "2026-06-08T08:10:00Z",
};

describe("matchRunComments", () => {
  it("keeps only the comments this task posted", () => {
    const timeline: TimelineEntry[] = [
      comment("mine", "task-1"),
      comment("other-run", "task-2"),
      comment("human", null),
      { type: "activity", id: "a", actor_type: "agent", actor_id: "agent-1", created_at: "2026-06-08T08:05:00Z", source_task_id: "task-1" },
    ];

    expect(matchRunComments(timeline, "task-1")).toEqual([
      { id: "mine", content: "comment mine", createdAt: "2026-06-08T08:05:00Z" },
    ]);
    expect(matchRunComments(undefined, "task-1")).toEqual([]);
  });
});

describe("matchRunPullRequests", () => {
  it("keeps pull requests opened while the run was going", () => {
    const prs = [
      pr("before", null, "2026-06-08T07:00:00Z"),
      pr("during", null, "2026-06-08T08:04:00Z"),
      pr("after", null, "2026-06-08T09:00:00Z"),
    ];

    expect(matchRunPullRequests(prs, run).map((p) => p.id)).toEqual(["during"]);
  });

  it("keeps a pull request on the run's branch whenever it was opened", () => {
    const prs = [pr("branch", "agent/j/abc", "2026-06-09T00:00:00Z"), pr("other", "main", "2026-06-09T00:00:00Z")];

    expect(matchRunPullRequests(prs, { ...run, branch_name: "agent/j/abc" }).map((p) => p.id)).toEqual(["branch"]);
  });

  it("treats a live run's window as open until now", () => {
    const prs = [pr("just-now", null, "2026-06-08T08:30:00Z")];
    const live = { ...run, completed_at: null };

    expect(matchRunPullRequests(prs, live, Date.parse("2026-06-08T08:31:00Z"))).toHaveLength(1);
    expect(matchRunPullRequests(prs, live, Date.parse("2026-06-08T08:20:00Z"))).toHaveLength(0);
  });

  it("matches nothing by time when the run never started", () => {
    const prs = [pr("any", null, "2026-06-08T08:04:00Z")];

    expect(matchRunPullRequests(prs, { ...run, started_at: null, dispatched_at: null })).toEqual([]);
  });
});
