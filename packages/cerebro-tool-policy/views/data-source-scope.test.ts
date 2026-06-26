import { describe, expect, it } from "vitest";
import { groupByFolder, type ScopeOption } from "./data-source-scope";

describe("groupByFolder", () => {
  it("groups options by folderId and sorts groups by folder name", () => {
    const opts: ScopeOption[] = [
      { id: "1", name: "Refunds", folder: "Finance", folderId: "f1" },
      { id: "2", name: "Headcount", folder: "HR", folderId: "f2" },
      { id: "3", name: "Orders", folder: "Finance", folderId: "f1" },
    ];
    const groups = groupByFolder(opts);
    expect(groups.map((g) => g.folder)).toEqual(["Finance", "HR"]);
    const finance = groups.find((g) => g.folderId === "f1")!;
    expect(finance.options.map((o) => o.id).sort()).toEqual(["1", "3"]);
  });

  it("buckets folderless options under an ungrouped group with empty folderId", () => {
    const opts: ScopeOption[] = [{ id: "x", name: "Loose" }];
    const groups = groupByFolder(opts);
    expect(groups).toHaveLength(1);
    expect(groups[0]!.folderId).toBe("");
  });
});
