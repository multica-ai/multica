import { describe, expect, it } from "vitest";
import type { WikiPageSummary } from "../types";
import { buildWikiTree, flattenWikiTree } from "./tree";

function page(partial: Partial<WikiPageSummary> & Pick<WikiPageSummary, "id" | "title">): WikiPageSummary {
  return {
    workspace_id: "ws-1",
    parent_id: null,
    slug: partial.id,
    position: 0,
    created_by: null,
    updated_by: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...partial,
  };
}

describe("wiki tree", () => {
  it("sorts roots and children by position then creation time", () => {
    const tree = buildWikiTree([
      page({ id: "b", title: "B", position: 1 }),
      page({ id: "a", title: "A", position: 0, created_at: "2026-01-02T00:00:00Z" }),
      page({ id: "c", title: "C", position: 0, created_at: "2026-01-01T00:00:00Z" }),
      page({ id: "a-2", title: "A2", parent_id: "a", position: 2 }),
      page({ id: "a-1", title: "A1", parent_id: "a", position: 1 }),
    ]);

    expect(tree.map((node) => node.id)).toEqual(["c", "a", "b"]);
    expect(tree[1]?.children.map((node) => node.id)).toEqual(["a-1", "a-2"]);
  });

  it("keeps pages with missing parents at the root and flattens preorder", () => {
    const tree = buildWikiTree([
      page({ id: "orphan", title: "Orphan", parent_id: "missing" }),
      page({ id: "root", title: "Root" }),
      page({ id: "child", title: "Child", parent_id: "root" }),
    ]);

    expect(tree.map((node) => node.id)).toEqual(["orphan", "root"]);
    expect(flattenWikiTree(tree).map((node) => node.id)).toEqual(["orphan", "root", "child"]);
  });
});
