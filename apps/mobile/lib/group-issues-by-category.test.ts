// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Issue } from "@multica/core/types";
import { groupIssuesByCategory } from "./group-issues-by-category";

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

describe("groupIssuesByCategory", () => {
  // The regression this exists for: bucketing by `issue.status` created a `qa`
  // bucket no section ever read, so the issue was simply not on the screen
  // while the header counts said nothing was wrong (MUL-6457).
  it("puts a custom status in its category's section", () => {
    const sections = groupIssuesByCategory([
      issue("a", "qa", "in_review"),
      issue("b", "in_review"),
    ]);
    expect(sections).toHaveLength(1);
    expect(sections[0].category).toBe("started");
    expect(sections[0].data.map((i) => i.id)).toEqual(["a", "b"]);
  });

  it("orders sections canonically and drops empty ones", () => {
    const sections = groupIssuesByCategory([
      issue("a", "done"),
      issue("b", "backlog"),
      issue("c", "in_progress"),
    ]);
    expect(sections.map((s) => s.category)).toEqual(["unstarted", "started", "done"]);
  });

  it("groups concrete built-ins into their lifecycle sections", () => {
    const sections = groupIssuesByCategory([
      issue("a", "todo"),
      issue("b", "todo"),
      issue("c", "blocked"),
    ]);
    expect(sections.map((s) => [s.category, s.data.length])).toEqual([
      ["unstarted", 2],
      ["started", 1],
    ]);
  });

  // Closed has no section on mobile. Legacy category values normalize to it.
  it("omits the closed category, built-in or custom", () => {
    expect(groupIssuesByCategory([issue("a", "cancelled")])).toEqual([]);
    expect(groupIssuesByCategory([issue("a", "wont_do", "cancelled")])).toEqual([]);
  });

  // Older backends predate `status_category`; a custom key then resolves to
  // nothing. Landing it in `unstarted` keeps the row on screen, which beats a row in
  // no section at all.
  it("still shows a custom status the payload could not resolve", () => {
    const sections = groupIssuesByCategory([issue("a", "qa")]);
    expect(sections.map((s) => s.category)).toEqual(["unstarted"]);
    expect(sections[0].data.map((i) => i.id)).toEqual(["a"]);
  });

  it("returns nothing for an empty list", () => {
    expect(groupIssuesByCategory([])).toEqual([]);
  });
});
