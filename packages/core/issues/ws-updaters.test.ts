import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import type { Issue, ListIssuesCache } from "../types";
import { issueKeys } from "./queries";
import { onIssueDeleted, onIssueUpdated } from "./ws-updaters";

const wsId = "ws-1";

function makeIssue(overrides: Partial<Issue> = {}): Issue {
  return {
    id: "issue-uuid-1",
    workspace_id: wsId,
    number: 124,
    identifier: "OPE-124",
    title: "Issue title",
    description: "Description",
    status: "todo",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    project_id: null,
    position: 0,
    due_date: null,
    created_at: "2026-04-27T07:31:04Z",
    updated_at: "2026-04-28T01:46:51Z",
    ...overrides,
  };
}

function makeList(issue: Issue): ListIssuesCache {
  return {
    byStatus: {
      [issue.status]: {
        issues: [issue],
        total: 1,
      },
    },
  };
}

describe("issue websocket updaters", () => {
  it("patches loaded issue details keyed by identifier", () => {
    const qc = new QueryClient();
    const issue = makeIssue();
    qc.setQueryData(issueKeys.list(wsId), makeList(issue));
    qc.setQueryData(issueKeys.detail(wsId, issue.identifier), issue);

    onIssueUpdated(qc, wsId, {
      id: issue.id,
      status: "in_progress",
      priority: "high",
    });

    expect(qc.getQueryData<Issue>(issueKeys.detail(wsId, issue.identifier))).toMatchObject({
      id: issue.id,
      status: "in_progress",
      priority: "high",
    });

    const list = qc.getQueryData<ListIssuesCache>(issueKeys.list(wsId));
    expect(list?.byStatus.todo?.issues).toEqual([]);
    expect(list?.byStatus.todo?.total).toBe(0);
    expect(list?.byStatus.in_progress?.issues[0]).toMatchObject({
      id: issue.id,
      status: "in_progress",
    });
    expect(list?.byStatus.in_progress?.total).toBe(1);
  });

  it("removes loaded issue details keyed by identifier", () => {
    const qc = new QueryClient();
    const issue = makeIssue();
    qc.setQueryData(issueKeys.detail(wsId, issue.id), issue);
    qc.setQueryData(issueKeys.detail(wsId, issue.identifier), issue);

    onIssueDeleted(qc, wsId, issue.id);

    expect(qc.getQueryData(issueKeys.detail(wsId, issue.id))).toBeUndefined();
    expect(qc.getQueryData(issueKeys.detail(wsId, issue.identifier))).toBeUndefined();
  });
});
