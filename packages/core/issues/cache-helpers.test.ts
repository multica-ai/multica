import { describe, expect, it } from "vitest";
import type { Issue, ListIssuesCache } from "../types";
import { insertByPosition, patchIssueInBuckets } from "./cache-helpers";

const WS_ID = "ws-1";

function mk(id: string, status: Issue["status"], position: number): Issue {
  return {
    id,
    workspace_id: WS_ID,
    number: 1,
    identifier: `MUL-${id}`,
    title: id,
    description: null,
    status,
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    project_id: null,
    position,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    labels: [],
    created_at: "2025-01-01T00:00:00Z",
    updated_at: "2025-01-01T00:00:00Z",
  };
}

function cache(byStatus: ListIssuesCache["byStatus"]): ListIssuesCache {
  return { byStatus };
}

function ids(c: ListIssuesCache, status: Issue["status"]): string[] {
  return (c.byStatus[status]?.issues ?? []).map((i) => i.id);
}

describe("insertByPosition", () => {
  it("inserts at the position-sorted slot", () => {
    const a = mk("a", "todo", 1);
    const c = mk("c", "todo", 3);
    const b = mk("b", "todo", 2);
    expect(insertByPosition([a, c], b).map((i) => i.id)).toEqual([
      "a",
      "b",
      "c",
    ]);
  });

  it("appends when the new position is the largest", () => {
    const a = mk("a", "todo", 1);
    const z = mk("z", "todo", 9);
    expect(insertByPosition([a], z).map((i) => i.id)).toEqual(["a", "z"]);
  });

  it("prepends when the new position is the smallest", () => {
    const b = mk("b", "todo", 2);
    const a = mk("a", "todo", 1);
    expect(insertByPosition([b], a).map((i) => i.id)).toEqual(["a", "b"]);
  });
});

describe("patchIssueInBuckets — cross-status move", () => {
  it("inserts the moved card at its position slot, not the end", () => {
    const c0 = cache({
      todo: { issues: [mk("moved", "todo", 5)], total: 1 },
      in_progress: {
        issues: [mk("x", "in_progress", 1), mk("y", "in_progress", 3)],
        total: 2,
      },
    });
    // Move "moved" into in_progress at position 2 (between x and y).
    const next = patchIssueInBuckets(c0, "moved", {
      status: "in_progress",
      position: 2,
    });
    expect(ids(next, "in_progress")).toEqual(["x", "moved", "y"]);
    expect(ids(next, "todo")).toEqual([]);
  });

  it("adjusts both bucket totals", () => {
    const c0 = cache({
      todo: { issues: [mk("moved", "todo", 5)], total: 1 },
      in_progress: { issues: [mk("x", "in_progress", 1)], total: 1 },
    });
    const next = patchIssueInBuckets(c0, "moved", {
      status: "in_progress",
      position: 2,
    });
    expect(next.byStatus.todo?.total).toBe(0);
    expect(next.byStatus.in_progress?.total).toBe(2);
  });
});

describe("patchIssueInBuckets — same status", () => {
  it("keeps the slot for a plain field update (no reorder)", () => {
    const c0 = cache({
      todo: {
        issues: [mk("a", "todo", 1), mk("b", "todo", 2), mk("c", "todo", 3)],
        total: 3,
      },
    });
    // A remote label/title edit must not move the card.
    const next = patchIssueInBuckets(c0, "b", { title: "renamed" });
    expect(ids(next, "todo")).toEqual(["a", "b", "c"]);
    expect(next.byStatus.todo?.issues[1]?.title).toBe("renamed");
  });

  it("re-sorts within the column when position changes", () => {
    const c0 = cache({
      todo: {
        issues: [mk("a", "todo", 1), mk("b", "todo", 2), mk("c", "todo", 3)],
        total: 3,
      },
    });
    // Drag "a" below "b" (new position 2.5).
    const next = patchIssueInBuckets(c0, "a", { position: 2.5 });
    expect(ids(next, "todo")).toEqual(["b", "a", "c"]);
  });
});

describe("patchIssueInBuckets — unknown issue", () => {
  it("returns the cache unchanged when the id is absent", () => {
    const c0 = cache({ todo: { issues: [mk("a", "todo", 1)], total: 1 } });
    expect(patchIssueInBuckets(c0, "ghost", { position: 9 })).toBe(c0);
  });
});
