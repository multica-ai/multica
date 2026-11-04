"use client";

import { useCallback } from "react";
import { toast } from "sonner";
import { planSeatAll, type CompanyFloor } from "@multica/core/company";
import type { Squad } from "@multica/core/types";
import { useT } from "../../i18n";
import { useSeatPlan } from "./use-seat-plan";

/**
 * "Seat all": puts every unseated squad in the next free room, adding floors
 * once the existing ones are full.
 */
export function useSeatAll(floors: readonly CompanyFloor[], squads: readonly Squad[]) {
  const { t } = useT("issues");
  const { commit, pending } = useSeatPlan(floors);

  const seatAll = useCallback(async () => {
    const plan = planSeatAll(floors, squads);
    if (plan.entries.length === 0) return;
    try {
      await commit(plan);
      toast.success(t(($) => $.company.seat_all_done, { count: plan.entries.length }));
      if (plan.skipped > 0) toast.error(t(($) => $.company.seat_all_skipped, { count: plan.skipped }));
    } catch {
      toast.error(t(($) => $.company.seat_failed));
    }
  }, [floors, squads, commit, t]);

  return { seatAll, pending };
}
