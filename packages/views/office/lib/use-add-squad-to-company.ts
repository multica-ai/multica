"use client";

import { useCallback } from "react";
import { toast } from "sonner";
import { planSeatAll, type OfficeFloor } from "@multica/core/office";
import type { Squad } from "@multica/core/types";
import { useT } from "../../i18n";
import { useSeatPlan } from "./use-seat-plan";

/**
 * "Add to company" for one squad outside the office: it takes the first free room, on the first
 * floor with one, or on a new floor when every floor is full. The squads already seated keep
 * their rooms.
 */
export function useAddSquadToCompany(floors: readonly OfficeFloor[], squads: readonly Squad[]) {
  const { t } = useT("issues");
  const { commit, pending } = useSeatPlan(floors);
  const floorIds = new Set(floors.map((floor) => floor.id));

  const addSquad = useCallback(
    async (squad: Squad) => {
      // Only this squad needs a seat: the planner sees the seated squads as taken, and nothing else unseated.
      const seated = (other: Squad) =>
        !other.archived_at && !!other.floor_id && floorIds.has(other.floor_id) && typeof other.office_slot === "number";
      const plan = planSeatAll(floors, squads.filter((other) => other.id === squad.id || seated(other)));
      if (plan.entries.length === 0) {
        toast.error(t(($) => $.office.seat_all_skipped, { count: 1 }));
        return;
      }
      try {
        await commit(plan);
        toast.success(t(($) => $.office.seat_done, { name: squad.name }));
      } catch {
        toast.error(t(($) => $.office.seat_failed));
      }
    },
    [floors, floorIds, squads, commit, t],
  );

  return { addSquad, pending };
}
