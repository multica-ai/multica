import { describe, expect, it } from "vitest";
import type { Issue } from "../types";
import {
  canMutateIssueThroughWorkspaceControl,
  issueWorkspaceControlWritable,
  updateTouchesWorkspaceControl,
} from "./workspace-control";

const baseIssue: Issue = {
  id: "issue-1",
  workspace_id: "workspace-1",
  number: 1,
  identifier: "MUL-1",
  title: "Issue",
  description: null,
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
  created_at: "2026-05-06T00:00:00Z",
  updated_at: "2026-05-06T00:00:00Z",
};

describe("workspace-control policy helpers", () => {
  it("treats native issues as writable", () => {
    expect(issueWorkspaceControlWritable(baseIssue)).toBe(true);
  });

  it("detects protected issue mutations", () => {
    expect(updateTouchesWorkspaceControl({ status: "in_progress" })).toBe(true);
    expect(updateTouchesWorkspaceControl({ assignee_id: null })).toBe(true);
    expect(updateTouchesWorkspaceControl({ due_date: null })).toBe(false);
  });

  it("blocks protected mutations for read-only Workspace sources", () => {
    const readonlyIssue: Issue = {
      ...baseIssue,
      workspace_control: {
        source_type: "ledger",
        source_id: "ledger:task-1",
        writable: false,
      },
    };

    expect(canMutateIssueThroughWorkspaceControl(readonlyIssue, { priority: "high" })).toBe(false);
    expect(canMutateIssueThroughWorkspaceControl(readonlyIssue, { due_date: null })).toBe(true);
  });
});
