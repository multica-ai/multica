// @vitest-environment node
import { describe, expect, it } from "vitest";
import { canMoveFolder, folderAncestry } from "./tree";
import type { DocFolder } from "../types";
const folder = (id: string, parentId: string | null): DocFolder => ({
  id,
  parentId,
  title: id,
  workspaceId: "ws",
  createdByType: "member",
  createdById: "m",
  createdAt: "",
  updatedAt: "",
});
describe("doc folder moves", () => {
  const folders = [folder("a", null), folder("b", "a"), folder("c", "b")];
  it("rejects self and descendants, permits root and ancestors", () => {
    expect(canMoveFolder(folders, "a", "a")).toBe(false);
    expect(canMoveFolder(folders, "a", "c")).toBe(false);
    expect(canMoveFolder(folders, "c", "a")).toBe(true);
    expect(canMoveFolder(folders, "b", null)).toBe(true);
    expect(folderAncestry(folders, "c").map((f) => f.id)).toEqual([
      "a",
      "b",
      "c",
    ]);
  });
});
