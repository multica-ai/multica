import { describe, expect, it } from "vitest";

import { clampSectionsToLayout, moveItem, moveSection } from "./strategy-board";
import type { VisionPlanItem, VisionPlanSection } from "./types";

const item = (id: string, sectionId: string, position: number, state: "active" | "archived" = "active"): VisionPlanItem => ({
  id, workspace_id: "ws", section_id: sectionId, title: id, description: "",
  position, state, goal_connections: [], links: [], created_at: "", updated_at: "",
});

const section = (
  id: string, position: number, items: VisionPlanItem[],
  { page = "vision", row = 0, column = 0 }: { page?: string; row?: number; column?: number } = {},
): VisionPlanSection => ({
  id, workspace_id: "ws", key: id, name: id, section_type: "list", position,
  page_id: page, row_index: row, column_index: column, items, created_at: "", updated_at: "",
});

const sections = (): VisionPlanSection[] => [
  section("a", 0, [item("a1", "a", 0), item("a2", "a", 1), item("a3", "a", 2)]),
  section("b", 1, [item("b1", "b", 0), item("b2", "b", 1)]),
  section("c", 2, []),
];

// Two blocks in row 0 column 0 and one in row 0 column 1 of the same page.
const blocks = (): VisionPlanSection[] => [
  section("a", 0, [], { column: 0 }),
  section("b", 1, [], { column: 0 }),
  section("c", 0, [], { column: 1 }),
];

describe("moveSection", () => {
  it("reorders blocks inside one cell", () => {
    expect(moveSection(blocks(), "b", "vision", 0, 0, "a")).toEqual([
      { id: "b", page_id: "vision", row_index: 0, column_index: 0, position: 0 },
      { id: "a", page_id: "vision", row_index: 0, column_index: 0, position: 1 },
    ]);
  });

  it("moves a block to another column and renumbers both", () => {
    expect(moveSection(blocks(), "a", "vision", 0, 1, "c")).toEqual(expect.arrayContaining([
      { id: "a", page_id: "vision", row_index: 0, column_index: 1, position: 0 },
      { id: "c", page_id: "vision", row_index: 0, column_index: 1, position: 1 },
      { id: "b", page_id: "vision", row_index: 0, column_index: 0, position: 0 },
    ]));
  });

  // FIR-3589 item 4: rows are their own drop targets, so a block can move down
  // into a row with a different column count.
  it("moves a block into another row", () => {
    expect(moveSection(blocks(), "c", "vision", 1, 0)).toEqual(expect.arrayContaining([
      { id: "c", page_id: "vision", row_index: 1, column_index: 0, position: 0 },
    ]));
  });

  it("moves a block to another page", () => {
    expect(moveSection(blocks(), "c", "traction", 0, 2)).toEqual(expect.arrayContaining([
      { id: "c", page_id: "traction", row_index: 0, column_index: 2, position: 0 },
    ]));
  });

  it("returns nothing for a no-op drop", () => {
    expect(moveSection(blocks(), "a", "vision", 0, 0, "a")).toEqual([]);
  });

  it("returns nothing for an unknown block", () => {
    expect(moveSection(blocks(), "missing", "vision", 0, 0)).toEqual([]);
  });
});

describe("clampSectionsToLayout", () => {
  // A row removed, or a row narrowed, must not make its blocks disappear.
  it("pulls blocks outside the layout back to the first cell", () => {
    const stranded = [
      section("a", 0, [], { row: 0, column: 0 }),
      section("b", 0, [], { row: 3, column: 0 }),
      section("c", 0, [], { row: 0, column: 2 }),
      section("d", 0, [], { page: "traction", row: 5, column: 5 }),
    ];
    const clamped = clampSectionsToLayout(stranded, "vision", [2, 1]);
    expect(clamped.map((s) => [s.id, s.row_index, s.column_index])).toEqual([
      ["a", 0, 0],
      ["b", 0, 0],
      ["c", 0, 0],
      ["d", 5, 5],
    ]);
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
    const withArchived: VisionPlanSection[] = [
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
