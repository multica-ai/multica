import { describe, expect, it } from "vitest";
import type { IssueView } from "../api/schemas";
import {
  defaultSidebarViewOrder,
  migrateSidebarViewOrder,
  reorderSidebarViewOrder,
  resolveSidebarViewItems,
} from "./sidebar-views";

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
  it("lists every built-in, own views, then previously pinned views once", () => {
    const views = [view("a", "me"), view("b", "other"), view("c", "other")];
    expect(defaultSidebarViewOrder(views, "me", ["c", "a"])).toEqual([
      "builtin:all_issues",
      "builtin:my_work",
      "builtin:working",
      "builtin:subscribed",
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

  it("drops built-ins that no longer exist", () => {
    const items = resolveSidebarViewItems(["builtin:unread", "builtin:subscribed"], []);
    expect(items).toEqual([{ id: "builtin:subscribed", kind: "builtin", key: "subscribed" }]);
  });
});

describe("migrateSidebarViewOrder", () => {
  it("turns My issues and My agents into My work, followed by the new built-ins, and drops Unread", () => {
    expect(
      migrateSidebarViewOrder([
        "view:a",
        "builtin:my_issues",
        "builtin:all_issues",
        "builtin:my_agents",
        "builtin:unread",
        "view:b",
      ]),
    ).toEqual([
      "view:a",
      "builtin:my_work",
      "builtin:working",
      "builtin:subscribed",
      "builtin:all_issues",
      "view:b",
    ]);
  });

  it("keeps a removed My issues removed, and leaves today's orders alone", () => {
    expect(migrateSidebarViewOrder(["builtin:all_issues", "builtin:unread"])).toEqual(["builtin:all_issues"]);
    const current = ["builtin:subscribed", "view:a"];
    expect(migrateSidebarViewOrder(current)).toEqual(current);
  });
});

describe("reorderSidebarViewOrder", () => {
  it("moves visible ids and keeps unresolved ones in their slots", () => {
    expect(
      reorderSidebarViewOrder(
        ["builtin:my_work", "view:loading", "view:a", "builtin:all_issues"],
        ["builtin:all_issues", "builtin:my_work", "view:a"],
      ),
    ).toEqual(["builtin:all_issues", "view:loading", "builtin:my_work", "view:a"]);
  });

  it("appends visible ids the stored order did not have", () => {
    expect(reorderSidebarViewOrder(["view:a", "view:a"], ["view:b", "view:a"])).toEqual(["view:b", "view:a"]);
  });
});
