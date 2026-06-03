import { describe, expect, it } from "vitest";
import type { Issue } from "@/shared/types";
import type { IssueDraft } from "@/features/issues/stores/draft-store";
import { buildIssueTemplateData, getCreateIssueInitialValues } from "./template";

function makeDraft(overrides: Partial<IssueDraft> = {}): IssueDraft {
  return {
    title: "Draft title",
    description: "Draft description",
    status: "todo",
    priority: "medium",
    assigneeType: "member",
    assigneeId: "user-1",
    parentIssueId: "parent-1",
    dueDate: "2026-05-01T08:00:00Z",
    startDate: "2026-04-30T08:00:00Z",
    endDate: "2026-05-02T08:00:00Z",
    ...overrides,
  };
}

function makeIssue(overrides: Partial<Issue> = {}): Issue {
  return {
    id: "issue-1",
    workspace_id: "ws-1",
    number: 7,
    identifier: "MUL-7",
    title: "Release prep",
    description: "Template description",
    status: "in_progress",
    priority: "high",
    assignee_type: "agent",
    assignee_id: "agent-1",
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: "parent-9",
    project_id: "project-3",
    position: 0,
    due_date: "2026-05-10T08:00:00Z",
    start_date: "2026-05-07T08:00:00Z",
    end_date: "2026-05-09T08:00:00Z",
    archived_at: null,
    archived_by: null,
    labels: [
      { id: "label-1", workspace_id: "ws-1", name: "Release", color: "blue" },
      { id: "label-2", workspace_id: "ws-1", name: "Ops", color: "green" },
    ],
    created_at: "2026-05-01T08:00:00Z",
    updated_at: "2026-05-01T08:00:00Z",
    ...overrides,
  };
}

describe("issue template helpers", () => {
  it("builds template defaults from an existing issue", () => {
    expect(buildIssueTemplateData(makeIssue())).toEqual({
      title: "Copy of Release prep",
      description: "Template description",
      status: "in_progress",
      priority: "high",
      assignee_type: "agent",
      assignee_id: "agent-1",
      parent_issue_id: "parent-9",
      project_id: "project-3",
      due_date: "2026-05-10T08:00:00Z",
      start_date: "2026-05-07T08:00:00Z",
      end_date: "2026-05-09T08:00:00Z",
      label_ids: ["label-1", "label-2"],
    });
  });

  it("prefers template values over an existing draft", () => {
    const values = getCreateIssueInitialValues(makeDraft(), {
      title: "Copy of Release prep",
      description: "Template description",
      status: "backlog",
      priority: "high",
      assignee_type: null,
      assignee_id: null,
      parent_issue_id: null,
      project_id: "project-3",
      due_date: null,
      start_date: null,
      end_date: null,
      label_ids: ["label-1", "label-1", "label-2"],
    });

    expect(values).toEqual({
      title: "Copy of Release prep",
      description: "Template description",
      status: "backlog",
      priority: "high",
      assigneeType: undefined,
      assigneeId: undefined,
      parentIssueId: undefined,
      projectId: "project-3",
      dueDate: null,
      startDate: null,
      endDate: null,
      labelIds: ["label-1", "label-2"],
    });
  });

  it("falls back to draft values when no prefill data is provided", () => {
    expect(getCreateIssueInitialValues(makeDraft())).toEqual({
      title: "Draft title",
      description: "Draft description",
      status: "todo",
      priority: "medium",
      assigneeType: "member",
      assigneeId: "user-1",
      parentIssueId: "parent-1",
      projectId: undefined,
      dueDate: "2026-05-01T08:00:00Z",
      startDate: "2026-04-30T08:00:00Z",
      endDate: "2026-05-02T08:00:00Z",
      labelIds: [],
    });
  });
});
