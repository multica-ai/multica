// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Issue } from "@multica/core/types";
import { groupIssuesByStatus } from "./group-issues-by-status";

function issue(id: string, status: string, statusCategory?: string): Issue {
  return {
    id,
    workspace_id: "ws-1",
    number: 1,
    identifier: `MUL-${id}`,
    title: id,
    description: null,
    status,
    ...(statusCategory ? { status_category: statusCategory as Issue["status_category"] } : {}),
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    project_id: null,
    position: 0,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    properties: {},
    created_at: "",
    updated_at: "",
  };
}

describe("groupIssuesByStatus", () => {
  // Every concrete status in the rows must produce its own readable section.
  it("keeps custom statuses separate from category siblings", () => {
    const sections = groupIssuesByStatus([
      issue("a", "qa", "in_review"),
      issue("b", "in_review"),
    ]);
    expect(sections).toHaveLength(2);
    expect(sections[0].status).toBe("in_review");
    expect(sections[1].status).toBe("qa");
    expect(sections[0].data.map((i) => i.id)).toEqual(["b"]);
    expect(sections[1].data.map((i) => i.id)).toEqual(["a"]);
  });

  it("orders sections canonically and drops empty ones", () => {
    const sections = groupIssuesByStatus([
      issue("a", "done"),
      issue("b", "backlog"),
      issue("c", "in_progress"),
    ]);
    expect(sections.map((s) => s.status)).toEqual(["backlog", "in_progress", "done"]);
  });

  it("groups rows sharing a concrete built-in status", () => {
    const sections = groupIssuesByStatus([
      issue("a", "todo"),
      issue("b", "todo"),
      issue("c", "blocked"),
    ]);
    expect(sections.map((s) => [s.status, s.data.length])).toEqual([
      ["todo", 2],
      ["blocked", 1],
    ]);
  });

  // Terminal lifecycle does not remove rows already selected by the filter.
  it("keeps cancelled work visible in its own status section", () => {
    expect(groupIssuesByStatus([issue("a", "cancelled")])[0].status).toBe("cancelled");
    expect(groupIssuesByStatus([issue("a", "wont_do", "cancelled")])[0].status).toBe("wont_do");
  });

  // A late catalog must not make a concrete custom status disappear.
  it("still shows a custom status the payload could not resolve", () => {
    const sections = groupIssuesByStatus([issue("a", "qa")]);
    expect(sections.map((s) => s.status)).toEqual(["qa"]);
    expect(sections[0].data.map((i) => i.id)).toEqual(["a"]);
  });

  it("returns nothing for an empty list", () => {
    expect(groupIssuesByStatus([])).toEqual([]);
  });
});
