import { COMPANY_SLOTS } from "@multica/core/company";
import type { Squad } from "@multica/core/types";

/**
 * The first room of `floorId` that no active squad holds, or null when the
 * floor is full. `squads` may include the squad being seated: it is not
 * counted against itself by the caller excluding it.
 */
export function firstFreeSlot(squads: readonly Squad[], floorId: string): number | null {
  const taken = new Set(
    squads
      .filter((squad) => !squad.archived_at && squad.floor_id === floorId && typeof squad.company_slot === "number")
      .map((squad) => squad.company_slot as number),
  );
  for (let slot = 0; slot < COMPANY_SLOTS; slot += 1) {
    if (!taken.has(slot)) return slot;
  }
  return null;
}
