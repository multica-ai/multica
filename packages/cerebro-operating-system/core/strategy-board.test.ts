import { describe, expect, it } from "vitest";

import { moveItem, moveSection } from "./strategy-board";
import type { VisionPlanItem, VisionPlanSection } from "./types";

const item = (id: string, sectionId: string, position: number, state: "active" | "archived" = "active"): VisionPlanItem => ({
  id, workspace_id: "ws", section_id: sectionId, title: id, description: "",
  position, state, goal_connections: [], links: [], created_at: "", updated_at: "",
});

const section = (id: string, position: number, items: VisionPlanItem[]): VisionPlanSection => ({
  id, workspace_id: "ws", key: id, name: id, section_type: "list", position, items,
  created_at: "", updated_at: "",
});

const sections = (): VisionPlanSection[] => [
  section("a", 0, [item("a1", "a", 0), item("a2", "a", 1), item("a3", "a", 2)]),
  section("b", 1, [item("b1", "b", 0), item("b2", "b", 1)]),
  section("c", 2, []),
];

describe("moveSection", () => {
  it("renumbers only the columns that shifted when moving right", () => {
    expect(moveSection(sections(), "a", "c")).toEqual([
      { id: "b", position: 0 },
      { id: "c", position: 1 },
      { id: "a", position: 2 },
    ]);
  });

  it("renumbers when moving left", () => {
    expect(moveSection(sections(), "c", "a")).toEqual([
      { id: "c", position: 0 },
      { id: "a", position: 1 },
      { id: "b", position: 2 },
    ]);
  });

  it("returns nothing for a no-op drop", () => {
    expect(moveSection(sections(), "a", "a")).toEqual([]);
  });
});

describe("moveItem", () => {
  it("reorders within a column", () => {
    // Drop a1 before a3 → order becomes a2, a1, a3.
    expect(moveItem(sections(), "a1", "a", "a3")).toEqual([
      { id: "a2", section_id: "a", position: 0 },
      { id: "a1", section_id: "a", position: 1 },
    ]);
  });

  it("moves an item to another column before a target", () => {
    // a1 leaves column a; b1 keeps position 0 so it is not re-emitted.
    const changes = moveItem(sections(), "a1", "b", "b2");
    expect(changes).toEqual(expect.arrayContaining([
      { id: "a2", section_id: "a", position: 0 },
      { id: "a3", section_id: "a", position: 1 },
      { id: "a1", section_id: "b", position: 1 },
      { id: "b2", section_id: "b", position: 2 },
    ]));
    expect(changes).toHaveLength(4);
  });

  it("appends to an empty column when no target item is given", () => {
    expect(moveItem(sections(), "b1", "c")).toEqual(expect.arrayContaining([
      { id: "b2", section_id: "b", position: 0 },
      { id: "b1", section_id: "c", position: 0 },
    ]));
  });

  it("ignores archived items when renumbering", () => {
    const withArchived = [
      section("a", 0, [item("a1", "a", 0), item("gone", "a", 1, "archived"), item("a2", "a", 2)]),
      section("b", 1, []),
    ];
    expect(moveItem(withArchived, "a1", "b")).toEqual(expect.arrayContaining([
      { id: "a2", section_id: "a", position: 0 },
      { id: "a1", section_id: "b", position: 0 },
    ]));
  });

  it("returns nothing when dropped on itself", () => {
    expect(moveItem(sections(), "a1", "a", "a1")).toEqual([]);
  });
});
