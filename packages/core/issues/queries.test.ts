import { describe, expect, it } from "vitest";
import { flattenIssueBuckets, issueKeys } from "./queries";
import type { Issue, ListIssuesCache } from "../types";

function issue(id: string): Issue {
  return {
    id,
    workspace_id: "workspace-1",
    identifier: id.toUpperCase(),
    title: id,
    description: "",
    status: "custom",
    priority: "medium",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "member-1",
    parent_issue_id: null,
    project_id: null,
    pipeline_id: null,
    position: 0,
    due_date: null,
    inherit_parent_workdir: false,
    created_at: "2026-04-25T00:00:00Z",
    updated_at: "2026-04-25T00:00:00Z",
    number: 1,
  };
}

describe("issue queries", () => {
  it("flattens custom pipeline statuses from bucketed cache", () => {
    const cache: ListIssuesCache = {
      byStatus: {
        "review-custom": { issues: [issue("issue-1")], total: 1 },
      },
    };

    expect(flattenIssueBuckets(cache).map((i) => i.id)).toEqual(["issue-1"]);
  });

  it("keys my-issues caches by status set", () => {
    expect(issueKeys.myList("ws", "assigned", {}, ["todo"])).not.toEqual(
      issueKeys.myList("ws", "assigned", {}, ["review-custom"]),
    );
  });
});
