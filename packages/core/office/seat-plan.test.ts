// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Squad } from "../types";
import { OFFICE_SLOTS } from "./constants";
import { freeSlots, planSeatAll } from "./seat-plan";

const FLOORS = [
  { id: "f2", position: 1 },
  { id: "f1", position: 0 },
];

function squad(id: string, seat?: { floor: string; slot: number }, archived = false): Squad {
  return {
    id,
    name: id,
    archived_at: archived ? "2026-01-01T00:00:00Z" : null,
    floor_id: seat?.floor ?? null,
    office_slot: seat?.slot ?? null,
  } as Squad;
}

describe("planSeatAll", () => {
  it("fills the lowest free slot of the lowest floor first", () => {
    const plan = planSeatAll(FLOORS, [squad("a", { floor: "f1", slot: 0 }), squad("b"), squad("c")]);
    expect(plan.entries).toEqual([
      { squadId: "b", floor: { id: "f1" }, slot: 1 },
      { squadId: "c", floor: { id: "f1" }, slot: 2 },
    ]);
    expect(plan.newFloors).toBe(0);
  });

  it("moves on to the next floor, then asks for new floors", () => {
    const squads = Array.from({ length: OFFICE_SLOTS * 2 + 2 }, (_, i) => squad(`s${i}`));
    const plan = planSeatAll(FLOORS, squads);
    expect(plan.entries[OFFICE_SLOTS]).toEqual({ squadId: `s${OFFICE_SLOTS}`, floor: { id: "f2" }, slot: 0 });
    expect(plan.entries[OFFICE_SLOTS * 2]).toEqual({ squadId: `s${OFFICE_SLOTS * 2}`, floor: { newFloor: 0 }, slot: 0 });
    expect(plan.entries[OFFICE_SLOTS * 2 + 1]!.slot).toBe(1);
    expect(plan.newFloors).toBe(1);
  });

  it("treats a squad on a missing floor or a taken seat as unseated, and ignores archived squads", () => {
    const plan = planSeatAll(FLOORS, [
      squad("a", { floor: "f1", slot: 0 }),
      squad("dup", { floor: "f1", slot: 0 }),
      squad("lost", { floor: "gone", slot: 3 }),
      squad("old", undefined, true),
    ]);
    expect(plan.entries.map((e) => e.squadId)).toEqual(["dup", "lost"]);
  });

  it("stops at the floor cap", () => {
    const plan = planSeatAll([{ id: "f1", position: 0 }], [squad("x"), squad("y")], 1);
    expect(plan.entries.map((e) => e.squadId)).toEqual(["x", "y"]);
    const full = planSeatAll(
      [{ id: "f1", position: 0 }],
      [...Array.from({ length: OFFICE_SLOTS }, (_, i) => squad(`k${i}`, { floor: "f1", slot: i })), squad("z")],
      1,
    );
    expect(full.entries).toEqual([]);
    expect(full.skipped).toBe(1);
  });
});

describe("freeSlots", () => {
  it("lists the slots no live squad holds", () => {
    expect(freeSlots("f1", [squad("a", { floor: "f1", slot: 1 }), squad("b", { floor: "f2", slot: 0 })])).toEqual([0, 2, 3, 4, 5]);
  });
});
