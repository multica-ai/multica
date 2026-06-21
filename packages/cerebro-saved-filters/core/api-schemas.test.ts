import { describe, expect, it } from "vitest";

import {
  EMPTY_SNAPSHOT,
  canShareSchema,
  savedFilterListSchema,
  savedFilterSchema,
} from "./api-schemas";
import { snapshotsEqual } from "./filter-snapshot";

describe("savedFilterSchema", () => {
  it("maps a well-formed row to camelCase", () => {
    const parsed = savedFilterSchema.parse({
      id: "f1",
      name: "My bugs",
      surface: "issues",
      filter_state: { projectFilters: ["p1"], includeNoAssignee: true },
      position: 2,
      created_at: "2026-06-20T10:00:00Z",
      updated_at: "2026-06-20T10:00:00Z",
    });
    expect(parsed.name).toBe("My bugs");
    expect(parsed.filterState.projectFilters).toEqual(["p1"]);
    expect(parsed.filterState.includeNoAssignee).toBe(true);
    // Missing snapshot fields default rather than throw.
    expect(parsed.filterState.statusFilters).toEqual([]);
  });

  it("falls back to an empty snapshot when filter_state is the wrong shape", () => {
    const parsed = savedFilterSchema.parse({
      id: "f2",
      name: "Broken",
      filter_state: "not-an-object",
      position: 0,
    });
    expect(snapshotsEqual(parsed.filterState, EMPTY_SNAPSHOT)).toBe(true);
    expect(parsed.surface).toBe("issues");
  });

  it("parses a list and tolerates missing optional fields", () => {
    const parsed = savedFilterListSchema.parse([
      { id: "a", name: "A", filter_state: {} },
    ]);
    expect(parsed).toHaveLength(1);
    expect(parsed[0]!.position).toBe(0);
    // Fase 3 fields default safely when an older backend omits them.
    expect(parsed[0]!.visibility).toBe("private");
    expect(parsed[0]!.isOwner).toBe(true);
  });

  it("maps Fase 3 sharing fields and tolerates unknown visibility", () => {
    const parsed = savedFilterSchema.parse({
      id: "f3",
      name: "Team filter",
      filter_state: {},
      visibility: "from-the-future",
      owner_id: "u1",
      owner_name: "Jesper",
      is_owner: false,
      shares: [{ target_type: "group", target_id: "g1" }],
    });
    // Unknown visibility downgrades to private rather than throwing.
    expect(parsed.visibility).toBe("private");
    expect(parsed.ownerId).toBe("u1");
    expect(parsed.ownerName).toBe("Jesper");
    expect(parsed.isOwner).toBe(false);
    expect(parsed.shares).toEqual([{ targetType: "group", targetId: "g1" }]);
  });
});

describe("canShareSchema", () => {
  it("reads the gate and defaults to false on a malformed body", () => {
    expect(canShareSchema.parse({ can_share: true })).toBe(true);
    expect(canShareSchema.parse({})).toBe(false);
    expect(canShareSchema.parse(null)).toBe(false);
  });
});

describe("snapshotsEqual", () => {
  it("is order-insensitive for arrays", () => {
    expect(
      snapshotsEqual(
        { ...EMPTY_SNAPSHOT, projectFilters: ["a", "b"] },
        { ...EMPTY_SNAPSHOT, projectFilters: ["b", "a"] },
      ),
    ).toBe(true);
  });

  it("distinguishes different selections", () => {
    expect(
      snapshotsEqual(
        { ...EMPTY_SNAPSHOT, includeNoAssignee: true },
        { ...EMPTY_SNAPSHOT, includeNoAssignee: false },
      ),
    ).toBe(false);
  });
});
