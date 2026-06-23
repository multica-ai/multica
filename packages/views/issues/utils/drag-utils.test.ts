import { describe, expect, it } from "vitest";
import type { Issue } from "@multica/core/types";
import { insertIdByPosition } from "./drag-utils";

function mk(id: string, position: number): Issue {
  return {
    id,
    workspace_id: "ws-1",
    number: 1,
    identifier: `MUL-${id}`,
    title: id,
    description: null,
    status: "todo",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    project_id: null,
    position,
    start_date: null,
    due_date: null,
    metadata: {},
    labels: [],
    created_at: "2025-01-01T00:00:00Z",
    updated_at: "2025-01-01T00:00:00Z",
  };
}

function mapOf(...issues: Issue[]): Map<string, Issue> {
  return new Map(issues.map((i) => [i.id, i]));
}

describe("insertIdByPosition", () => {
  it("inserts the id at its position-sorted slot", () => {
    const map = mapOf(mk("a", 1), mk("c", 3), mk("b", 2));
    expect(insertIdByPosition(["a", "c"], "b", 2, map)).toEqual([
      "a",
      "b",
      "c",
    ]);
  });

  it("appends when the position is the largest", () => {
    const map = mapOf(mk("a", 1), mk("z", 9));
    expect(insertIdByPosition(["a"], "z", 9, map)).toEqual(["a", "z"]);
  });

  it("prepends when the position is the smallest", () => {
    const map = mapOf(mk("b", 2), mk("a", 1));
    expect(insertIdByPosition(["b"], "a", 1, map)).toEqual(["a", "b"]);
  });

  it("appends into an empty target column", () => {
    const map = mapOf(mk("a", 5));
    expect(insertIdByPosition([], "a", 5, map)).toEqual(["a"]);
  });

  it("matches insertByPosition ordering so the settle rebuild is a no-op", () => {
    // Same scenario the board's optimistic drop and the cache patch both apply:
    // landing a card between two neighbours must produce the same order in the
    // id list (board) and the issue list (cache).
    const map = mapOf(mk("x", 1), mk("y", 3), mk("moved", 2));
    expect(insertIdByPosition(["x", "y"], "moved", 2, map)).toEqual([
      "x",
      "moved",
      "y",
    ]);
  });
});
