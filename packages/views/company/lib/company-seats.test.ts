// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Squad } from "@multica/core/types";
import { firstFreeSlot } from "./company-seats";

const squad = (id: string, floor: string | null, slot: number | null, archived = false) =>
  ({ id, floor_id: floor, company_slot: slot, archived_at: archived ? "2026-01-01" : null }) as Squad;

describe("firstFreeSlot", () => {
  it("takes the lowest room the floor does not hold", () => {
    const squads = [squad("a", "f1", 0), squad("b", "f1", 2), squad("c", "f2", 1)];
    expect(firstFreeSlot(squads, "f1")).toBe(1);
    expect(firstFreeSlot(squads, "f2")).toBe(0);
  });

  it("ignores archived squads and squads on other floors", () => {
    const squads = [squad("a", "f1", 0, true), squad("b", "f2", 0)];
    expect(firstFreeSlot(squads, "f1")).toBe(0);
  });

  it("returns null when every room is taken", () => {
    const squads = Array.from({ length: 6 }, (_, slot) => squad(`s${slot}`, "f1", slot));
    expect(firstFreeSlot(squads, "f1")).toBeNull();
  });
});
