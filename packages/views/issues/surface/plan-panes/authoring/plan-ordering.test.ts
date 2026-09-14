// @vitest-environment node

/**
 * Canonical coverage for the two pure ordering helpers. The reorder contract
 * is strict — `validateExactOrder` in
 * server/internal/projectplan/service.go rejects anything that is not a
 * complete permutation of the current siblings — so the boundary matrix lives
 * here rather than being re-run through a DOM mount.
 */

import { describe, expect, it } from "vitest";
import { movedOrder, nextPosition } from "./plan-ordering";

describe("nextPosition", () => {
  it("is 0 for the first sibling", () => {
    expect(nextPosition([])).toBe(0);
  });

  it("is one past the highest position in use", () => {
    expect(nextPosition([{ position: 0 }, { position: 1 }, { position: 2 }])).toBe(3);
  });

  it("clears a gap left by a deletion instead of colliding with a survivor", () => {
    // Positions 0 and 5 survive; a length-based guess would return 2, which is
    // free, but after deleting position 0 it would return 1 while position 5
    // still exists — this test pins the max-based rule that never collides.
    expect(nextPosition([{ position: 0 }, { position: 5 }])).toBe(6);
  });

  it("does not assume positions arrive sorted", () => {
    expect(nextPosition([{ position: 4 }, { position: 1 }])).toBe(5);
  });
});

describe("movedOrder", () => {
  const siblings = [{ id: "a" }, { id: "b" }, { id: "c" }];

  it("moves an item up one slot and returns the complete order", () => {
    expect(movedOrder(siblings, "c", "up")).toEqual(["a", "c", "b"]);
  });

  it("moves an item down one slot and returns the complete order", () => {
    expect(movedOrder(siblings, "a", "down")).toEqual(["b", "a", "c"]);
  });

  it("returns null at the top boundary rather than an unchanged permutation", () => {
    expect(movedOrder(siblings, "a", "up")).toBeNull();
  });

  it("returns null at the bottom boundary", () => {
    expect(movedOrder(siblings, "c", "down")).toBeNull();
  });

  it("returns null for an id that is not a sibling", () => {
    expect(movedOrder(siblings, "z", "up")).toBeNull();
  });

  it("returns null for a single-item list in either direction", () => {
    expect(movedOrder([{ id: "only" }], "only", "up")).toBeNull();
    expect(movedOrder([{ id: "only" }], "only", "down")).toBeNull();
  });

  it("never drops or duplicates an id", () => {
    const result = movedOrder(siblings, "b", "down");
    expect(result).toHaveLength(siblings.length);
    expect(new Set(result)).toEqual(new Set(["a", "b", "c"]));
  });
});
