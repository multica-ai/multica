import { describe, expect, it } from "vitest";
import { NO_PROJECT_ORDER_ID } from "../issues/sidebar-list";
import { sidebarFolderOrder, sidebarProjectRowIds, sidebarProjectRows, type SidebarFolder } from "./folder-tree";

const folder = (id: string, name: string): SidebarFolder => ({ id, name });
const project = (id: string, folder_id: string | null = null) => ({ id, folder_id });
/** Where `projects` are filed, as an extension reports it. */
const filedIn = (projects: readonly { id: string; folder_id: string | null }[]) =>
  new Map(projects.flatMap((p) => (p.folder_id ? [[p.id, p.folder_id] as const] : [])));

const billing = folder("f-billing", "Billing");
const growth = folder("f-growth", "Growth");
const byName = { ordering: "name", showNoProject: false } as const;
const manual = { ordering: "manual", showNoProject: false } as const;

const shape = (rows: ReturnType<typeof sidebarProjectRows>) =>
  rows.map((row) => (row.kind === "folder" ? [row.id, row.projects.map((p) => p.id)] : row.id));

describe("sidebarProjectRows", () => {
  it("files shown projects under their folders, folders first by name", () => {
    const shown = [project("a"), project("b", "f-growth"), project("c", "f-billing"), project("d", "f-growth")];
    const rows = sidebarProjectRows(shown, [growth, billing], filedIn(shown), byName, []);
    expect(shape(rows)).toEqual([
      ["f-billing", ["c"]],
      ["f-growth", ["b", "d"]],
      "a",
    ]);
  });

  it("hides a folder the settings filter empties, but keeps one nothing is filed in", () => {
    const all = [project("a"), project("hidden", "f-billing")];
    const rows = sidebarProjectRows([project("a")], [billing, growth], filedIn(all), byName, []);
    expect(shape(rows)).toEqual([["f-growth", []], "a"]);
  });

  it("treats a project whose folder is gone as outside any folder", () => {
    const shown = [project("a", "deleted-folder")];
    expect(shape(sidebarProjectRows(shown, [], filedIn(shown), byName, []))).toEqual(["a"]);
  });

  it("puts No project first unless the manual order places it", () => {
    const shown = [project("a"), project("b", "f-billing")];
    const settings = { ordering: "name", showNoProject: true } as const;
    expect(shape(sidebarProjectRows(shown, [billing], filedIn(shown), settings, []))).toEqual([
      NO_PROJECT_ORDER_ID,
      ["f-billing", ["b"]],
      "a",
    ]);
    const placed = sidebarProjectRows(shown, [billing], filedIn(shown), { ...settings, ordering: "manual" }, [
      "a",
      "f-billing",
      "b",
      NO_PROJECT_ORDER_ID,
    ]);
    expect(shape(placed)).toEqual(["a", ["f-billing", ["b"]], NO_PROJECT_ORDER_ID]);
  });

  it("orders folders among projects by the manual order, new folders first", () => {
    const shown = [project("a"), project("b"), project("c", "f-billing")];
    const rows = sidebarProjectRows(shown, [billing, growth], filedIn(shown), manual, ["a", "f-billing", "c", "b"]);
    expect(shape(rows)).toEqual([["f-growth", []], "a", ["f-billing", ["c"]], "b"]);
  });
});

describe("sidebarProjectRowIds", () => {
  it("lists each folder's projects right after it", () => {
    const shown = [project("a"), project("b", "f-billing"), project("c", "f-billing")];
    const rows = sidebarProjectRows(shown, [billing], filedIn(shown), manual, ["a", "f-billing"]);
    expect(sidebarProjectRowIds(rows)).toEqual(["a", "f-billing", "b", "c"]);
  });
});

describe("sidebarFolderOrder", () => {
  const sales = folder("f-sales", "Sales");

  it("lists folders in the sidebar's order", () => {
    const folders = [sales, growth, billing];
    const order = ["a", "f-sales", "b", "f-billing"];
    for (const settings of [byName, manual]) {
      const rows = sidebarProjectRows([], folders, new Map(), settings, order);
      expect(sidebarFolderOrder(folders, settings, order).map((f) => f.id)).toEqual(rows.map((row) => row.id));
    }
    expect(sidebarFolderOrder(folders, manual, order).map((f) => f.id)).toEqual(["f-growth", "f-sales", "f-billing"]);
  });
});
