// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { DocFolder, DocSummary } from "../types";
import { groupDocActivity } from "./activity";
import { buildDocTree, canMoveFolder, folderAncestry } from "./tree";

const folder = (id: string, title: string, parent_id: string | null = null): DocFolder => ({
  id,
  workspace_id: "ws",
  parent_id,
  title,
  created_by_type: "member",
  created_by_id: "u",
  created_at: "",
  updated_at: "",
});

const doc = (id: string, title: string, folder_id: string | null, unread = false): DocSummary => ({
  id,
  workspace_id: "ws",
  folder_id,
  format: "markdown",
  title,
  revision: 1,
  version_count: 1,
  created_by_type: "member",
  created_by_id: "u",
  updated_by_type: "member",
  updated_by_id: "u",
  created_at: "",
  updated_at: "",
  unread,
});

describe("buildDocTree", () => {
  const folders = [folder("eng", "Engineering"), folder("be", "Backend", "eng"), folder("ops", "Ops")];
  const docs = [doc("d1", "Deploy", "be", true), doc("d2", "Readme", null), doc("d3", "Orphan", "gone")];

  it("nests folders first, sorts by title and counts unread per folder", () => {
    const tree = buildDocTree({ folders, docs });
    expect(tree.map((n) => (n.kind === "folder" ? n.folder.title : n.doc.title))).toEqual([
      "Engineering",
      "Ops",
      "Orphan",
      "Readme",
    ]);
    const eng = tree[0]!;
    expect(eng.kind === "folder" && eng.unread).toBe(1);
    const be = eng.kind === "folder" ? eng.children[0]! : null;
    expect(be?.kind === "folder" && be.children.map((c) => (c.kind === "doc" ? c.doc.id : ""))).toEqual(["d1"]);
  });

  it("walks ancestry and refuses cyclic moves", () => {
    expect(folderAncestry(folders, "be").map((f) => f.id)).toEqual(["eng", "be"]);
    expect(canMoveFolder(folders, "eng", "be")).toBe(false);
    expect(canMoveFolder(folders, "eng", "eng")).toBe(false);
    expect(canMoveFolder(folders, "be", "ops")).toBe(true);
    expect(canMoveFolder(folders, "be", null)).toBe(true);
  });
});

describe("groupDocActivity", () => {
  const item = (doc_id: string, number: number, updated_at: string, extra: Record<string, unknown> = {}) => ({
    doc_id,
    doc_title: doc_id.toUpperCase(),
    doc_folder_id: null,
    doc_format: "markdown" as const,
    number,
    author_type: "member" as const,
    author_id: "u1",
    message: "",
    action: "edit" as const,
    action_ref: null,
    task_id: null,
    issue_id: null,
    lines_added: 1,
    lines_removed: 0,
    created_at: updated_at,
    updated_at,
    unread: false,
    ...extra,
  });

  it("folds versions into one row per doc per day", () => {
    const days = groupDocActivity([
      item("a", 3, "2026-10-29T15:00:00", { author_type: "agent", author_id: "bot", message: "sync API", unread: true }),
      item("b", 1, "2026-10-29T12:00:00", { action: "create" }),
      item("a", 2, "2026-10-29T10:00:00"),
      item("a", 1, "2026-10-28T10:00:00", { action: "create" }),
    ]);
    expect(days.map((d) => d.day)).toEqual(["2026-10-29", "2026-10-28"]);
    const a = days[0]!.rows[0]!;
    expect(a).toMatchObject({ docId: "a", edits: 2, unread: true, firstVersion: 2, latestVersion: 3 });
    expect(a.authors).toEqual([{ type: "agent", id: "bot" }, { type: "member", id: "u1" }]);
    expect(a.messages).toEqual([{ authorType: "agent", authorId: "bot", text: "sync API" }]);
    expect(days[0]!.rows[1]).toMatchObject({ docId: "b", created: true });
  });

  it("keeps only unread rows when filtering", () => {
    const days = groupDocActivity(
      [item("a", 2, "2026-10-29T15:00:00", { unread: true }), item("b", 1, "2026-10-28T12:00:00")],
      true,
    );
    expect(days).toHaveLength(1);
    expect(days[0]!.rows.map((r) => r.docId)).toEqual(["a"]);
  });
});
