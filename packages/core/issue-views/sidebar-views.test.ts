import { describe, expect, it } from "vitest";
import type { IssueView } from "../api/schemas";
import {
  availableBuiltinViewKeys,
  defaultSidebarViewOrder,
  reorderSidebarViewOrder,
  resolveSidebarViewItems,
  withIntroducedBuiltins,
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
  it("lists built-ins, own views, then previously pinned views once", () => {
    const views = [view("a", "me"), view("b", "other"), view("c", "other")];
    expect(defaultSidebarViewOrder(views, "me", ["c", "a"])).toEqual([
      "builtin:my_issues",
      "builtin:my_agents",
      "builtin:all_issues",
      "builtin:awaiting_merge",
      "view:a",
      "view:c",
    ]);
  });
});

describe("availableBuiltinViewKeys", () => {
  it("offers Awaiting merge only once a code host is known to be connected", () => {
    expect(availableBuiltinViewKeys(true).has("awaiting_merge")).toBe(true);
    expect(availableBuiltinViewKeys(false).has("awaiting_merge")).toBe(false);
    expect(availableBuiltinViewKeys(undefined).has("awaiting_merge")).toBe(false);
    expect([...availableBuiltinViewKeys(false)]).toEqual(["my_issues", "my_agents", "all_issues"]);
  });
});

describe("withIntroducedBuiltins", () => {
  const all = availableBuiltinViewKeys(true);

  it("puts a new built-in after the last built-in the user kept", () => {
    expect(withIntroducedBuiltins(["view:a", "builtin:all_issues", "view:b"], [], all)).toEqual([
      "view:a",
      "builtin:all_issues",
      "builtin:awaiting_merge",
      "view:b",
    ]);
  });

  it("appends it when the user kept no built-ins", () => {
    expect(withIntroducedBuiltins(["view:a"], [], all)).toEqual(["view:a", "builtin:awaiting_merge"]);
  });

  it("does not bring back a built-in the user was already offered", () => {
    expect(withIntroducedBuiltins(["builtin:my_issues"], ["awaiting_merge"], all)).toEqual(["builtin:my_issues"]);
  });

  it("waits for a code host before offering Awaiting merge", () => {
    expect(withIntroducedBuiltins(["builtin:my_issues"], [], availableBuiltinViewKeys(false))).toEqual([
      "builtin:my_issues",
    ]);
  });

  it("never adds a second copy", () => {
    const order = ["builtin:awaiting_merge", "builtin:my_issues"];
    expect(withIntroducedBuiltins(order, [], all)).toEqual(order);
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

  it("hides built-ins the workspace cannot use", () => {
    const order = ["builtin:my_issues", "builtin:awaiting_merge"];
    expect(resolveSidebarViewItems(order, [], availableBuiltinViewKeys(false)).map((item) => item.id)).toEqual([
      "builtin:my_issues",
    ]);
    expect(resolveSidebarViewItems(order, [], availableBuiltinViewKeys(true)).map((item) => item.id)).toEqual(order);
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
