import { describe, expect, it } from "vitest";
import { applyBoxSettings, sortNotes } from "./inbox-box-filter";
import type { Note } from "./types";

function note(p: Partial<Note>): Note {
  return {
    id: p.id ?? "",
    workspace_id: "",
    folder_id: null,
    title: p.title ?? "",
    body: p.body ?? "",
    owner_id: "",
    visibility: p.visibility ?? "private",
    pinned: p.pinned ?? false,
    created_at: p.created_at ?? "2026-01-01T00:00:00Z",
    updated_at: p.updated_at ?? "2026-01-01T00:00:00Z",
  };
}

const a = note({ id: "a", pinned: true, visibility: "private", updated_at: "2026-06-01T00:00:00Z", created_at: "2026-01-01T00:00:00Z" });
const b = note({ id: "b", pinned: false, visibility: "workspace", updated_at: "2026-06-10T00:00:00Z", created_at: "2026-05-01T00:00:00Z" });
const c = note({ id: "c", pinned: false, visibility: "shared", updated_at: "2026-06-05T00:00:00Z", created_at: "2026-06-01T00:00:00Z" });
const all = [b, c, a]; // intentionally unsorted

describe("applyBoxSettings", () => {
  it("defaults to pinned-first then most-recently updated", () => {
    const out = applyBoxSettings(all, { limit: 10, pinnedOnly: false, sort: "pinned" });
    expect(out.map((n) => n.id)).toEqual(["a", "b", "c"]);
  });

  it("sorts by latest changed (updated_at) ignoring pin", () => {
    const out = applyBoxSettings(all, { limit: 10, pinnedOnly: false, sort: "updated" });
    expect(out.map((n) => n.id)).toEqual(["b", "c", "a"]);
  });

  it("sorts by newest (created_at)", () => {
    const out = applyBoxSettings(all, { limit: 10, pinnedOnly: false, sort: "created" });
    expect(out.map((n) => n.id)).toEqual(["c", "b", "a"]);
  });

  it("filters to pinned only", () => {
    const out = applyBoxSettings(all, { limit: 10, pinnedOnly: true, sort: "pinned" });
    expect(out.map((n) => n.id)).toEqual(["a"]);
  });

  it("filters by visibility set", () => {
    const out = applyBoxSettings(all, {
      limit: 10,
      pinnedOnly: false,
      sort: "pinned",
      visibility: ["workspace", "shared"],
    });
    expect(out.map((n) => n.id).sort()).toEqual(["b", "c"]);
  });

  it("treats empty visibility as all", () => {
    const out = applyBoxSettings(all, { limit: 10, pinnedOnly: false, sort: "pinned", visibility: [] });
    expect(out).toHaveLength(3);
  });

  it("caps to limit after sorting", () => {
    const out = applyBoxSettings(all, { limit: 2, pinnedOnly: false, sort: "pinned" });
    expect(out.map((n) => n.id)).toEqual(["a", "b"]);
  });

  it("does not mutate the input array", () => {
    const input = [b, c, a];
    sortNotes(input, "updated");
    expect(input.map((n) => n.id)).toEqual(["b", "c", "a"]);
  });
});
