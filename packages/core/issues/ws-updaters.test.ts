import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import { issueKeys } from "./queries";
import { onIssueUpdated } from "./ws-updaters";
import type { Issue, ListIssuesResponse } from "../types";

const workspaceId = "ws_1";
const parentId = "issue_parent";

function makeIssue(
  overrides: Partial<Issue> & Pick<Issue, "id" | "title">,
): Issue {
  const { id, title, ...rest } = overrides;

  return {
    id,
    workspace_id: workspaceId,
    number: 1,
    identifier: "MUL-1",
    title,
    description: null,
    status: "todo",
    priority: "medium",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "member_1",
    parent_issue_id: null,
    project_id: null,
    position: 0,
    due_date: null,
    archived_at: null,
    archived_by: null,
    created_at: "2026-04-13T00:00:00Z",
    updated_at: "2026-04-13T00:00:00Z",
    ...rest,
  };
}

function makeQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
}

describe("issue websocket updaters", () => {
  it("removes archived children from child caches and invalidates for refetch", () => {
    const qc = makeQueryClient();
    const child = makeIssue({
      id: "issue_child",
      title: "Child",
      parent_issue_id: parentId,
      status: "done",
    });
    const sibling = makeIssue({
      id: "issue_sibling",
      title: "Sibling",
      parent_issue_id: parentId,
    });
    qc.setQueryData<Issue[]>(issueKeys.children(workspaceId, parentId), [
      child,
      sibling,
    ]);
    qc.setQueryData<ListIssuesResponse>(issueKeys.list(workspaceId), {
      issues: [child, sibling],
      total: 2,
      doneTotal: 1,
    });
    const invalidateSpy = vi.spyOn(qc, "invalidateQueries");

    onIssueUpdated(
      qc,
      workspaceId,
      {
        id: child.id,
        parent_issue_id: parentId,
        archived_at: "2026-04-13T12:00:00Z",
        archived_by: "member_1",
      },
      { archived: true },
    );

    expect(
      qc.getQueryData<Issue[]>(issueKeys.children(workspaceId, parentId)),
    ).toEqual([sibling]);
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: issueKeys.children(workspaceId, parentId),
    });
  });

  it("invalidates child caches when restored children are missing from cache", () => {
    const qc = makeQueryClient();
    const child = makeIssue({
      id: "issue_child",
      title: "Child",
      parent_issue_id: parentId,
      archived_at: "2026-04-13T12:00:00Z",
      archived_by: "member_1",
    });
    const sibling = makeIssue({
      id: "issue_sibling",
      title: "Sibling",
      parent_issue_id: parentId,
    });
    qc.setQueryData<Issue[]>(issueKeys.children(workspaceId, parentId), [
      sibling,
    ]);
    qc.setQueryData<Issue>(issueKeys.detail(workspaceId, child.id), child);
    const invalidateSpy = vi.spyOn(qc, "invalidateQueries");

    onIssueUpdated(
      qc,
      workspaceId,
      {
        id: child.id,
        parent_issue_id: parentId,
        archived_at: null,
        archived_by: null,
      },
      { restored: true },
    );

    expect(
      qc.getQueryData<Issue[]>(issueKeys.children(workspaceId, parentId)),
    ).toEqual([sibling]);
    expect(
      qc.getQueryData<Issue>(issueKeys.detail(workspaceId, child.id))?.archived_at,
    ).toBeNull();
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: issueKeys.children(workspaceId, parentId),
    });
  });

  it("updates cached children without child-list refetch for ordinary updates", () => {
    const qc = makeQueryClient();
    const child = makeIssue({
      id: "issue_child",
      title: "Child",
      parent_issue_id: parentId,
    });
    qc.setQueryData<Issue[]>(issueKeys.children(workspaceId, parentId), [child]);
    qc.setQueryData<ListIssuesResponse>(issueKeys.list(workspaceId), {
      issues: [child],
      total: 1,
      doneTotal: 0,
    });
    const invalidateSpy = vi.spyOn(qc, "invalidateQueries");

    onIssueUpdated(qc, workspaceId, {
      ...child,
      title: "Updated child",
      archived_at: null,
      archived_by: null,
    });

    expect(
      qc.getQueryData<Issue[]>(issueKeys.children(workspaceId, parentId)),
    ).toEqual([{ ...child, title: "Updated child" }]);
    expect(invalidateSpy).not.toHaveBeenCalledWith({
      queryKey: issueKeys.children(workspaceId, parentId),
    });
  });
});
