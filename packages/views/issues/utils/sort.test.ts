import { describe, it, expect, vi } from "vitest";
import type { Issue } from "@multica/core/types";
import { sortIssues } from "./sort";

vi.mock("@multica/core/issues/config", () => ({
  PRIORITY_ORDER: ["urgent", "high", "medium", "low", "none"],
}));

function issue(over: Partial<Issue> & { id: string }): Issue {
  return {
    workspace_id: "ws-1",
    number: 1,
    identifier: "MUL-1",
    title: over.id,
    description: "",
    status: "todo",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    project_id: null,
    position: 0,
    start_date: null,
    due_date: null,
    metadata: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...over,
  };
}

const ids = (xs: Issue[]) => xs.map((x) => x.id);

describe("sortIssues", () => {
  const unordered = [
    issue({ id: "b", position: 20 }),
    issue({ id: "a", position: 10 }),
    issue({ id: "c", position: 30 }),
  ];

  it("orders by position ascending", () => {
    expect(ids(sortIssues(unordered, "position", "asc"))).toEqual(["a", "b", "c"]);
  });

  it("ignores direction for position (manual order is directionless)", () => {
    // A stale "desc" left over from a prior field-sort must not reverse the
    // manual order — the server never applies a direction to position.
    expect(ids(sortIssues(unordered, "position", "desc"))).toEqual(["a", "b", "c"]);
  });

  it("still honors direction for field sorts", () => {
    const byTitle = [
      issue({ id: "a", title: "Alpha" }),
      issue({ id: "c", title: "Charlie" }),
      issue({ id: "b", title: "Bravo" }),
    ];
    expect(ids(sortIssues(byTitle, "title", "asc"))).toEqual(["a", "b", "c"]);
    expect(ids(sortIssues(byTitle, "title", "desc"))).toEqual(["c", "b", "a"]);
  });
});
