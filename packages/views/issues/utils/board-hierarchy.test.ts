// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Issue } from "@multica/core/types";
import {
  buildBoardColumnTree,
  nestedIssueIds,
  siblingIds,
  visibleTreeIssueIds,
  type BoardTreeNode,
} from "./board-hierarchy";

function mk(id: string, parent: string | null = null): Issue {
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
    parent_issue_id: parent,
    project_id: null,
    position: 0,
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

function mapOf(...issues: Issue[]) {
  return new Map(issues.map((issue) => [issue.id, issue]));
}

/** Compact shape: "id" for a card, "~id" for a ghost, children in brackets. */
function shape(nodes: BoardTreeNode[]): unknown[] {
  return nodes.map((node) => {
    const label = node.kind === "issue" ? node.issue.id : `~${node.parentId}`;
    return node.children.length ? [label, shape(node.children)] : label;
  });
}

const none = () => undefined;

describe("board column tree", () => {
  it("puts sub-issues inside their parent's card, recursively", () => {
    const map = mapOf(mk("epic"), mk("x"), mk("feature", "epic"), mk("task", "feature"));
    const tree = buildBoardColumnTree(["epic", "x", "feature", "task"], map, none);
    expect(shape(tree)).toEqual([["epic", [["feature", ["task"]]]], "x"]);
    expect(tree[0]!.descendantCount).toBe(2);
  });

  it("wraps a sub-issue whose parent is in another column in a ghost", () => {
    const map = mapOf(mk("p"), mk("c1", "p"), mk("c2", "p"), mk("x"));
    const tree = buildBoardColumnTree(["x", "c1", "c2"], map, none);
    expect(shape(tree)).toEqual(["x", ["~p", ["c1", "c2"]]]);
    const ghost = tree[1]!;
    expect(ghost.kind === "ghost" && ghost.parent?.id).toBe("p");
  });

  it("resolves a ghost's parent through the lookup when it is not on the board", () => {
    const parent = mk("p");
    const map = mapOf(mk("c", "p"));
    const tree = buildBoardColumnTree(["c"], map, (id) => (id === "p" ? parent : undefined));
    expect(tree[0]).toMatchObject({ kind: "ghost", parent });
  });

  it("keeps an unresolved ghost with an unknown parent", () => {
    const tree = buildBoardColumnTree(["c"], mapOf(mk("c", "gone")), none);
    expect(tree[0]).toMatchObject({ kind: "ghost", parentId: "gone", parent: undefined });
  });

  it("places a ghost inside the grandparent's card when that is in the column", () => {
    const map = mapOf(mk("epic"), mk("feature", "epic"), mk("task", "feature"));
    const tree = buildBoardColumnTree(["epic", "task"], map, none);
    expect(shape(tree)).toEqual([["epic", [["~feature", ["task"]]]]]);
  });

  it("orders a ghost where its first sub-issue appears", () => {
    const map = mapOf(mk("a"), mk("c", "p"), mk("b"));
    expect(shape(buildBoardColumnTree(["a", "c", "b"], map, none))).toEqual([
      "a",
      ["~p", ["c"]],
      "b",
    ]);
  });

  it("never drops a card caught in a corrupt parent cycle", () => {
    const map = mapOf(mk("a", "b"), mk("b", "a"));
    const ids = visibleTreeIssueIds(buildBoardColumnTree(["a", "b"], map, none), new Set());
    expect(ids.sort()).toEqual(["a", "b"]);
  });

  it("lists visible cards in display order, skipping collapsed contents", () => {
    const map = mapOf(mk("p"), mk("c", "p"), mk("g", "c"), mk("o", "elsewhere"));
    const tree = buildBoardColumnTree(["p", "c", "g", "o"], map, none);
    expect(visibleTreeIssueIds(tree, new Set())).toEqual(["p", "c", "g", "o"]);
    expect(visibleTreeIssueIds(tree, new Set(["c"]))).toEqual(["p", "c", "o"]);
    expect(visibleTreeIssueIds(tree, new Set(["elsewhere"]))).toEqual(["p", "c", "g"]);
  });

  it("orders a card only against cards sharing its parent", () => {
    const map = mapOf(mk("p"), mk("c1", "p"), mk("c2", "p"), mk("x"));
    const ids = ["p", "c1", "x", "c2"];
    expect(siblingIds(ids, "x", map)).toEqual(["p", "x"]);
    expect(siblingIds(ids, "c2", map)).toEqual(["c1", "c2"]);
  });
  it("collects every sub-issue inside a card, at any depth, but nothing outside it", () => {
    const map = mapOf(
      mk("p"),
      mk("c", "p"),
      mk("g", "c"),
      mk("e", "elsewhere"),
      mk("x"),
      mk("orphan", "p-elsewhere-child"),
      mk("p-elsewhere-child", "p"),
    );
    // "p-elsewhere-child" lives in another column, so its child "orphan"
    // sits in a ghost inside p's card here and travels along with p.
    const ids = ["p", "c", "g", "e", "x", "orphan"];
    expect(nestedIssueIds(ids, map, none, "p")).toEqual(["c", "g", "orphan"]);
    expect(nestedIssueIds(ids, map, none, "c")).toEqual(["g"]);
    expect(nestedIssueIds(ids, map, none, "x")).toEqual([]);
  });
});
