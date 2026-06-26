import { describe, expect, it } from "vitest";
import {
  buildFolderTree,
  collectSubtreeIds,
  flattenFolderTree,
} from "./tree";
import type { CollectionFolder } from "./api";

function folder(p: Partial<CollectionFolder>): CollectionFolder {
  return {
    surface: p.surface ?? "artifact",
    id: p.id ?? "",
    name: p.name ?? "",
    group: p.group ?? "Documents",
    parent_id: p.parent_id ?? null,
  };
}

describe("buildFolderTree", () => {
  it("nests children under their parent and sorts by name at each level", () => {
    const flat = [
      folder({ id: "root-b", name: "Bravo" }),
      folder({ id: "root-a", name: "Alpha" }),
      folder({ id: "child-2", name: "Zeta", parent_id: "root-a" }),
      folder({ id: "child-1", name: "Mike", parent_id: "root-a" }),
    ];
    const tree = buildFolderTree(flat);
    expect(tree.map((n) => n.id)).toEqual(["root-a", "root-b"]);
    const alpha = tree[0]!;
    expect(alpha.depth).toBe(0);
    expect(alpha.children.map((n) => n.id)).toEqual(["child-1", "child-2"]);
    expect(alpha.children[0]!.depth).toBe(1);
  });

  it("surfaces folders whose parent is absent from the list as roots", () => {
    // parent on another surface, or a filtered-out parent — must not vanish.
    const flat = [folder({ id: "orphan", name: "Orphan", parent_id: "gone" })];
    const tree = buildFolderTree(flat);
    expect(tree.map((n) => n.id)).toEqual(["orphan"]);
  });

  it("does not loop forever on a self-referencing parent_id", () => {
    const flat = [folder({ id: "loop", name: "Loop", parent_id: "loop" })];
    const tree = buildFolderTree(flat);
    expect(tree.map((n) => n.id)).toEqual(["loop"]);
    expect(tree[0]!.children).toEqual([]);
  });
});

describe("flattenFolderTree", () => {
  it("returns depth-first render order", () => {
    const flat = [
      folder({ id: "a", name: "Alpha" }),
      folder({ id: "a1", name: "Alpha-1", parent_id: "a" }),
      folder({ id: "b", name: "Bravo" }),
    ];
    const ordered = flattenFolderTree(buildFolderTree(flat));
    expect(ordered.map((n) => n.id)).toEqual(["a", "a1", "b"]);
  });
});

describe("collectSubtreeIds", () => {
  it("includes the node and every descendant", () => {
    const flat = [
      folder({ id: "a", name: "A" }),
      folder({ id: "a1", name: "A1", parent_id: "a" }),
      folder({ id: "a1x", name: "A1X", parent_id: "a1" }),
      folder({ id: "b", name: "B" }),
    ];
    const tree = buildFolderTree(flat);
    const a = tree.find((n) => n.id === "a")!;
    expect(collectSubtreeIds(a)).toEqual(new Set(["a", "a1", "a1x"]));
  });
});
