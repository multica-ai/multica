// @vitest-environment node
import { expect, it } from "vitest";
import type { Workspace } from "../types";
import { sortWorkspaces } from "./sort-workspaces";

const workspace = (id: string, position?: number | null, pinned_at: string | null = null) => ({
  id,
  workspace_position: position,
  pinned_at,
  created_at: `2026-01-0${id}T00:00:00Z`,
}) as Workspace;

it("preserves the complete legacy order when no member has a position", () => {
  const input = [workspace("4"), workspace("3", null, "2026-03-02T00:00:00Z"), workspace("2"), workspace("1", null, "2026-03-01T00:00:00Z")];
  const legacy = [...input].sort((a, b) => {
    if (a.pinned_at && b.pinned_at) return b.pinned_at.localeCompare(a.pinned_at);
    if (a.pinned_at) return -1;
    if (b.pinned_at) return 1;
    return a.created_at.localeCompare(b.created_at);
  });
  expect(sortWorkspaces(input)).toEqual(legacy);
});

it("orders each pin group by saved position, with unpositioned workspaces last", () => {
  const input = [workspace("1"), workspace("2", 1), workspace("3", 0), workspace("4", 3, "2026-03-01T00:00:00Z"), workspace("5", null, "2026-04-01T00:00:00Z")];
  expect(sortWorkspaces(input).map((item) => item.id)).toEqual(["4", "5", "3", "2", "1"]);
  expect(input.map((item) => item.id)).toEqual(["1", "2", "3", "4", "5"]);
});

it("keeps positions when pinning and unpinning a workspace", () => {
  const first = workspace("1", 1);
  const second = workspace("2", 0);
  expect(sortWorkspaces([first, second]).map((item) => item.id)).toEqual(["2", "1"]);
  expect(sortWorkspaces([{ ...first, pinned_at: "2026-03-01T00:00:00Z" }, second]).map((item) => item.id)).toEqual(["1", "2"]);
  expect(sortWorkspaces([first, second]).map((item) => item.id)).toEqual(["2", "1"]);
});
