import { describe, expect, it } from "vitest";
import type { Issue } from "@multica/core/types";
import type { BoardColumnGroup } from "../components/board-column";
import {
  buildColumns,
  getIssueGroupId,
  getMoveAnchors,
  getMoveUpdates,
  insertIdByPosition,
  issueMatchesGroup,
  projectGroupId,
  propertyGroupId,
} from "./drag-utils";

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
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    properties: {},
    labels: [],
    created_at: "2025-01-01T00:00:00Z",
    updated_at: "2025-01-01T00:00:00Z",
  };
}

function mapOf(...issues: Issue[]): Map<string, Issue> {
  return new Map(issues.map((i) => [i.id, i]));
}

describe("getMoveAnchors", () => {
  it("derives relative neighbors from the optimistic order", () => {
    expect(getMoveAnchors(["a", "moving", "b"], "moving")).toEqual({
      before_id: "a",
      after_id: "b",
    });
    expect(getMoveAnchors(["moving"], "moving")).toEqual({
      before_id: null,
      after_id: null,
    });
  });
});

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

/**
 * Status columns are CATEGORIES while `issue.status` is a concrete KEY. Every
 * assertion here is a card on a custom status: bucketing it by the raw key
 * produced a column id no column has, so the card was dropped from the board
 * and the list — filtering by that status left a visibly empty column next to
 * a non-zero header count (MUL-6409).
 */
describe("status grouping with custom statuses", () => {
  const custom = {
    ...mk("custom", 1),
    status: "awaiting_response",
    status_category: "in_review",
  } as Issue;
  const builtIn = { ...mk("built-in", 2), status: "in_review" } as Issue;
  const inReviewColumn: BoardColumnGroup = {
    id: "status:in_review",
    title: "In Review",
    status: "in_review",
  };
  const todoColumn: BoardColumnGroup = { id: "status:todo", title: "Todo", status: "todo" };
  const doneColumn: BoardColumnGroup = { id: "status:done", title: "Done", status: "done" };

  it("buckets a custom status into its category's column", () => {
    expect(getIssueGroupId(custom, "status")).toBe("status:in_review");
    expect(getIssueGroupId(builtIn, "status")).toBe("status:in_review");
  });

  it("renders the card in that column instead of dropping it", () => {
    const columns = buildColumns([custom, builtIn], [inReviewColumn], "status");
    expect(columns["status:in_review"]).toEqual(["custom", "built-in"]);
  });

  it("treats the card as already in the column it is drawn in", () => {
    expect(issueMatchesGroup(custom, inReviewColumn)).toBe(true);
    expect(issueMatchesGroup(custom, todoColumn)).toBe(false);
  });

  // A status change starts an agent run, so a same-column reorder that rewrote
  // `awaiting_response` to `in_review` would be a silent, side-effecting edit.
  it("reorders within the column without rewriting the status", () => {
    expect(getMoveUpdates(inReviewColumn, 5, custom)).toEqual({ position: 5 });
  });

  it("still sets the status when the card moves to another column", () => {
    expect(getMoveUpdates(doneColumn, 5, custom)).toEqual({ status: "done", position: 5 });
  });

  it("sets the status when the caller has no issue to compare", () => {
    expect(getMoveUpdates(inReviewColumn, 5)).toEqual({ status: "in_review", position: 5 });
  });

  // A built-in card in its own column keeps carrying the (unchanged) key, so a
  // workspace without custom statuses sends exactly the payload it always did.
  it("keeps the status in the payload for a built-in card", () => {
    expect(getMoveUpdates(inReviewColumn, 5, builtIn)).toEqual({
      status: "in_review",
      position: 5,
    });
  });
});

describe("property grouping", () => {
  const propertyId = "prop-env";
  const withValue = { id: "A", properties: { [propertyId]: "opt-staging" } } as unknown as Issue;
  const withoutValue = { id: "B", properties: {} } as unknown as Issue;

  it("getIssueGroupId buckets by option id, no-value issues into the none column", () => {
    expect(getIssueGroupId(withValue, `property:${propertyId}`)).toBe(
      propertyGroupId(propertyId, "opt-staging"),
    );
    expect(getIssueGroupId(withoutValue, `property:${propertyId}`)).toBe(
      propertyGroupId(propertyId, null),
    );
  });

  it("issueMatchesGroup distinguishes option and no-value columns", () => {
    const optionColumn = { id: "c1", title: "Staging", propertyId, propertyOptionId: "opt-staging" };
    const noneColumn = { id: "c2", title: "No value", propertyId, propertyOptionId: null };
    expect(issueMatchesGroup(withValue, optionColumn)).toBe(true);
    expect(issueMatchesGroup(withValue, noneColumn)).toBe(false);
    expect(issueMatchesGroup(withoutValue, noneColumn)).toBe(true);
  });

  it("unknown option values bucket into the none column when the catalog is known", () => {
    const stale = { id: "C", properties: { [propertyId]: "opt-deleted" } } as unknown as Issue;
    const known = new Set(["opt-staging"]);
    expect(getIssueGroupId(stale, `property:${propertyId}`, known)).toBe(
      propertyGroupId(propertyId, null),
    );
    // Without the catalog, the raw bucket is preserved (caller may still map it).
    expect(getIssueGroupId(stale, `property:${propertyId}`)).toBe(
      propertyGroupId(propertyId, "opt-deleted"),
    );
  });

  it("getMoveUpdates for property columns only carries position", () => {
    expect(getMoveUpdates({ id: "c1", title: "Staging", propertyId, propertyOptionId: "opt-staging" }, 5)).toEqual({ position: 5 });
  });
});

describe("project grouping", () => {
  const inProject = { id: "A", project_id: "proj-1" } as unknown as Issue;
  const noProject = { id: "B", project_id: null } as unknown as Issue;
  const projectColumn: BoardColumnGroup = {
    id: projectGroupId("proj-1"),
    title: "Acme",
    projectId: "proj-1",
  };
  const noProjectColumn: BoardColumnGroup = {
    id: projectGroupId(null),
    title: "No project",
    projectId: null,
  };

  it("projectGroupId mirrors the server group keys", () => {
    // The board builds column ids from cards while the server builds them from
    // descriptors; a mismatch silently drops every card off the board.
    expect(projectGroupId("proj-1")).toBe("project:proj-1");
    expect(projectGroupId(null)).toBe("project:none");
  });

  it("getIssueGroupId buckets unassigned-project cards into the none column", () => {
    expect(getIssueGroupId(inProject, "project")).toBe(projectGroupId("proj-1"));
    expect(getIssueGroupId(noProject, "project")).toBe(projectGroupId(null));
  });

  it("issueMatchesGroup distinguishes project and no-project columns", () => {
    expect(issueMatchesGroup(inProject, projectColumn)).toBe(true);
    expect(issueMatchesGroup(inProject, noProjectColumn)).toBe(false);
    expect(issueMatchesGroup(noProject, noProjectColumn)).toBe(true);
    expect(issueMatchesGroup(noProject, projectColumn)).toBe(false);
  });

  it("getMoveUpdates writes the column's project, clearing it on the none column", () => {
    expect(getMoveUpdates(projectColumn, 5)).toEqual({
      project_id: "proj-1",
      position: 5,
    });
    expect(getMoveUpdates(noProjectColumn, 5)).toEqual({
      project_id: null,
      position: 5,
    });
  });

  it("a project column never falls through to the unassigned-assignee update", () => {
    // projectId/assigneeId are both optional on BoardColumnGroup, so an
    // unguarded project column would read as "no assignee" and unassign the
    // card on every drop.
    expect(getMoveUpdates(projectColumn, 5)).not.toHaveProperty("assignee_type");
  });

  it("buildColumns places cards into their project column", () => {
    expect(
      buildColumns([inProject, noProject], [projectColumn, noProjectColumn], "project"),
    ).toEqual({
      [projectGroupId("proj-1")]: ["A"],
      [projectGroupId(null)]: ["B"],
    });
  });
});
