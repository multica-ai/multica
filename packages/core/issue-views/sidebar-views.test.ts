import { describe, expect, it } from "vitest";
import type { IssueView } from "../api/schemas";
import { defaultSidebarViewOrder, reorderSidebarViewOrder, resolveSidebarViewItems } from "./sidebar-views";

const view = (id: string, owner_id: string): IssueView => ({
  id,
  owner_id,
  workspace_id: "ws",
  name: id,
  scope_type: "workspace",
  scope_id: null,
  scope_variant: null,
  visibility: "workspace",
  definition_version: 1,
  query: {},
  display: {},
  revision: 1,
  created_at: "",
  updated_at: "",
});

describe("defaultSidebarViewOrder", () => {
  it("lists built-ins, own views, then previously pinned views once", () => {
    const views = [view("a", "me"), view("b", "other"), view("c", "other")];
    expect(defaultSidebarViewOrder(views, "me", ["c", "a"])).toEqual([
      "builtin:my_issues",
      "builtin:my_agents",
      "builtin:all_issues",
      "view:a",
      "view:c",
    ]);
  });
});

describe("resolveSidebarViewItems", () => {
  it("keeps the saved order and drops unknown or duplicate ids", () => {
    const items = resolveSidebarViewItems(
      ["view:b", "builtin:all_issues", "view:gone", "builtin:nope", "view:b"],
      [view("a", "me"), view("b", "me")],
    );
    expect(items.map((item) => item.id)).toEqual(["view:b", "builtin:all_issues"]);
    expect(items[1]).toMatchObject({ kind: "builtin", key: "all_issues" });
  });
});

describe("reorderSidebarViewOrder", () => {
  it("moves visible ids and keeps unresolved ones in their slots", () => {
    expect(
      reorderSidebarViewOrder(
        ["builtin:my_issues", "view:loading", "view:a", "builtin:all_issues"],
        ["builtin:all_issues", "builtin:my_issues", "view:a"],
      ),
    ).toEqual(["builtin:all_issues", "view:loading", "builtin:my_issues", "view:a"]);
  });

  it("appends visible ids the stored order did not have", () => {
    expect(reorderSidebarViewOrder(["view:a", "view:a"], ["view:b", "view:a"])).toEqual(["view:b", "view:a"]);
  });
});
