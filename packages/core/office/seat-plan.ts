import type { Squad } from "../types";
import { OFFICE_MAX_FLOORS, OFFICE_SLOTS } from "./constants";
import type { OfficeFloor } from "./types";

/** Where one unseated squad goes: a floor that exists, or the n-th floor still to be created. */
export interface SeatPlanEntry {
  squadId: string;
  floor: { id: string } | { newFloor: number };
  slot: number;
}

export interface SeatPlan {
  entries: SeatPlanEntry[];
  /** Floors to create first; `newFloor` in the entries counts from 0 in creation order. */
  newFloors: number;
  /** Squads left out because the workspace is at its floor cap. */
  skipped: number;
}

function validSlot(value: unknown): value is number {
  return typeof value === "number" && Number.isInteger(value) && value >= 0 && value < OFFICE_SLOTS;
}

/**
 * Plans "Seat all": every live squad without a usable seat goes, in the order
 * given, into the lowest free slot of the lowest floor, then onto new floors
 * once every floor is full. Pure: nothing is written.
 */
export function planSeatAll(
  floors: readonly Pick<OfficeFloor, "id" | "position">[],
  squads: readonly Squad[],
  maxFloors: number = OFFICE_MAX_FLOORS,
): SeatPlan {
  const ordered = [...floors].sort((a, b) => a.position - b.position);
  const floorIds = new Set(ordered.map((floor) => floor.id));
  const taken = new Set<string>();
  const unseated: Squad[] = [];
  for (const squad of squads) {
    if (squad.archived_at) continue;
    const key = `${squad.floor_id}:${squad.office_slot}`;
    if (squad.floor_id && floorIds.has(squad.floor_id) && validSlot(squad.office_slot) && !taken.has(key)) {
      taken.add(key);
    } else {
      unseated.push(squad);
    }
  }

  const entries: SeatPlanEntry[] = [];
  let newFloors = 0;
  let skipped = 0;
  let floorIndex = 0;
  let slot = 0;
  const advance = () => {
    slot += 1;
    if (slot === OFFICE_SLOTS) {
      floorIndex += 1;
      slot = 0;
    }
  };
  for (const squad of unseated) {
    while (floorIndex < ordered.length && taken.has(`${ordered[floorIndex]!.id}:${slot}`)) advance();
    if (floorIndex < ordered.length) {
      entries.push({ squadId: squad.id, floor: { id: ordered[floorIndex]!.id }, slot });
    } else {
      const newFloor = floorIndex - ordered.length;
      if (ordered.length + newFloor >= maxFloors) {
        skipped += 1;
        continue;
      }
      entries.push({ squadId: squad.id, floor: { newFloor }, slot });
      newFloors = Math.max(newFloors, newFloor + 1);
    }
    advance();
  }
  return { entries, newFloors, skipped };
}

/** Free slots of one floor, lowest first. */
export function freeSlots(floorId: string, squads: readonly Squad[]): number[] {
  const taken = new Set(
    squads
      .filter((squad) => !squad.archived_at && squad.floor_id === floorId && validSlot(squad.office_slot))
      .map((squad) => squad.office_slot as number),
  );
  return Array.from({ length: OFFICE_SLOTS }, (_, slot) => slot).filter((slot) => !taken.has(slot));
}
