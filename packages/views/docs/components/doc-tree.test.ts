// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { DocFolder, DocSummary } from "@multica/core/types";
import { canDropInto, folderAndDescendants } from "./doc-tree";

const folders = [
  { id: "eng", parentId: null },
  { id: "eng-api", parentId: "eng" },
  { id: "eng-api-v2", parentId: "eng-api" },
  { id: "ops", parentId: null },
];
const folder = (id: string) =>
  ({ kind: "folder", item: { ...folders.find((f) => f.id === id)!, title: id } as DocFolder }) as const;
const doc = (folderId: string | null) =>
  ({ kind: "doc", item: { id: "d", title: "Runbook", folderId } as DocSummary }) as const;

describe("folderAndDescendants", () => {
  it("collects a folder and every folder beneath it", () => {
    expect([...folderAndDescendants(folders, "eng")].sort()).toEqual(["eng", "eng-api", "eng-api-v2"]);
  });
});

describe("canDropInto", () => {
  it("moves a doc anywhere but where it already is", () => {
    expect(canDropInto(doc("eng"), "ops", folders)).toBe(true);
    expect(canDropInto(doc("eng"), null, folders)).toBe(true);
    expect(canDropInto(doc("eng"), "eng", folders)).toBe(false);
  });

  it("never moves a folder into itself or beneath itself", () => {
    expect(canDropInto(folder("eng"), "ops", folders)).toBe(true);
    expect(canDropInto(folder("eng"), "eng", folders)).toBe(false);
    expect(canDropInto(folder("eng"), "eng-api-v2", folders)).toBe(false);
    expect(canDropInto(folder("eng-api"), null, folders)).toBe(true);
    expect(canDropInto(folder("eng-api"), "eng", folders)).toBe(false);
  });
});
