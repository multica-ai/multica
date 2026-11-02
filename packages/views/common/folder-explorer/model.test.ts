// @vitest-environment node

import { describe, expect, it } from "vitest";
import {
  directChildFolders,
  directEntries,
  entryPathHints,
  flattenFolderExplorerRows,
  folderEntryCounts,
  folderPath,
  folderPathLabel,
  isManagedLocation,
  type FolderExplorerEntry,
  type FolderExplorerFolder,
} from "./model";

function folder(
  id: string,
  name = id,
  parentId: string | null = null,
  managed = false,
): FolderExplorerFolder {
  return { id, name, parentId, managed };
}

function entry(id: string, ...folderIds: string[]): FolderExplorerEntry {
  return { id, folderIds };
}

const folders = [
  folder("z", "Zeta"),
  folder("a", "Alpha"),
  folder("b", "Beta", "a"),
  folder("managed", "Package", null, true),
  folder("inside", "Inside", "managed"),
];

describe("direct folder explorer contents", () => {
  it("sorts only direct child folders by name", () => {
    expect(directChildFolders(folders, null).map((item) => item.id)).toEqual([
      "a",
      "managed",
      "z",
    ]);
    expect(directChildFolders(folders, "a").map((item) => item.id)).toEqual(["b"]);
    expect(directChildFolders(folders, "b")).toEqual([]);
  });

  it("surfaces an orphan at root instead of hiding it", () => {
    expect(directChildFolders([folder("orphan", "Orphan", "missing")], null)).toHaveLength(1);
  });

  it("returns direct entries and supports multiple memberships", () => {
    const entries = [entry("loose"), entry("one", "a"), entry("many", "a", "managed")];
    expect(directEntries(entries, null, folders).map((item) => item.id)).toEqual(["loose"]);
    expect(directEntries(entries, "a", folders).map((item) => item.id)).toEqual(["one", "many"]);
    expect(directEntries(entries, "managed", folders).map((item) => item.id)).toEqual(["many"]);
  });

  it("keeps entries whose folders are all missing visible at root", () => {
    const entries = [entry("orphan", "missing"), entry("placed", "a"), entry("mixed", "a", "missing")];
    expect(directEntries(entries, null, folders).map((item) => item.id)).toEqual(["orphan"]);
  });
});

describe("folder paths and managed locations", () => {
  it("builds a root-to-leaf path and a flat-view hint", () => {
    expect(folderPath(folders, "b").map((item) => item.id)).toEqual(["a", "b"]);
    expect(folderPathLabel(folders, "b")).toBe("Alpha / Beta");
    expect(folderPathLabel(folders, "missing")).toBe("");
  });

  it("returns every non-root path for multi-folder entries", () => {
    expect(entryPathHints(folders, entry("many", "b", "managed"))).toEqual([
      "Alpha / Beta",
      "Package",
    ]);
    expect(entryPathHints(folders, entry("loose"))).toEqual([]);
  });

  it("treats descendants of a managed folder as managed", () => {
    expect(isManagedLocation(folders, null)).toBe(false);
    expect(isManagedLocation(folders, "a")).toBe(false);
    expect(isManagedLocation(folders, "managed")).toBe(true);
    expect(isManagedLocation(folders, "inside")).toBe(true);
  });
});

describe("subtree entry counts", () => {
  it("counts entries throughout the subtree without double-counting memberships", () => {
    const counts = folderEntryCounts(folders, [
      entry("loose"),
      entry("alpha", "a"),
      entry("beta", "b"),
      entry("twice", "a", "b"),
    ]);
    expect(counts.get("a")).toBe(3);
    expect(counts.get("b")).toBe(2);
    expect(counts.get("z")).toBe(0);
  });
});

describe("in-place folder explorer rows", () => {
  const entries = [
    entry("loose"),
    entry("one", "a"),
    entry("many", "a", "managed"),
    entry("beta", "b"),
  ];

  it("starts collapsed and keeps root folders before root entries", () => {
    const rows = flattenFolderExplorerRows({
      folders,
      entries,
      expandedFolderIds: new Set(),
    });
    expect(rows.map((row) => row.kind === "folder" ? row.folder.id : row.entry.id)).toEqual([
      "a", "managed", "z", "loose",
    ]);
    expect(rows.every((row) => row.depth === 0)).toBe(true);
  });

  it("flattens expanded descendants depth-first with parent-scoped keys", () => {
    const rows = flattenFolderExplorerRows({
      folders,
      entries,
      expandedFolderIds: new Set(["a", "b", "managed"]),
      entryKeyPrefix: "agent",
    });
    expect(rows.map((row) => [row.kind, row.kind === "folder" ? row.folder.id : row.entry.id, row.depth])).toEqual([
      ["folder", "a", 0],
      ["folder", "b", 1],
      ["entry", "beta", 2],
      ["entry", "one", 1],
      ["entry", "many", 1],
      ["folder", "managed", 0],
      ["folder", "inside", 1],
      ["entry", "many", 1],
      ["folder", "z", 0],
      ["entry", "loose", 0],
    ]);
    const repeated = rows.filter((row) => row.kind === "entry" && row.entry.id === "many");
    expect(repeated.map((row) => row.key)).toEqual(["agent-a-many", "agent-managed-many"]);
    expect(new Set(rows.map((row) => row.key)).size).toBe(rows.length);
  });

  it("surfaces orphan folders and ignores unreachable cycles", () => {
    const rows = flattenFolderExplorerRows({
      folders: [folder("orphan", "Orphan", "missing"), folder("cycle", "Cycle", "cycle")],
      entries: [],
      expandedFolderIds: new Set(["orphan", "cycle"]),
    });
    expect(rows.map((row) => row.kind === "folder" && row.folder.id)).toEqual(["orphan"]);
  });
});
