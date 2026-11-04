"use client";

import { useCallback, useState } from "react";
import { useCreateCompanyFloor, useSetCompanySeats, type CompanyFloor, type SeatPlan } from "@multica/core/company";
import { useT } from "../../i18n";

/**
 * Writes a seat plan: floors the plan needs are created first (each awaited, as the server can
 * refuse), then every seat goes in one request. Throws when the server refuses, for the caller to
 * report.
 */
export function useSeatPlan(floors: readonly CompanyFloor[]) {
  const { t } = useT("issues");
  const createFloor = useCreateCompanyFloor();
  const setSeats = useSetCompanySeats();
  const [pending, setPending] = useState(false);

  const commit = useCallback(
    async (plan: SeatPlan) => {
      if (plan.entries.length === 0) return;
      setPending(true);
      try {
        const used = new Set(floors.map((floor) => floor.name));
        const created: string[] = [];
        let n = floors.length;
        for (let i = 0; i < plan.newFloors; i += 1) {
          let name: string;
          do {
            n += 1;
            name = t(($) => $.company.floor_default_name, { n });
          } while (used.has(name));
          used.add(name);
          created.push((await createFloor.mutateAsync({ name })).id);
        }
        await setSeats.mutateAsync(
          plan.entries.map((entry) => ({
            squad_id: entry.squadId,
            floor_id: "id" in entry.floor ? entry.floor.id : created[entry.floor.newFloor]!,
            slot: entry.slot,
          })),
        );
      } finally {
        setPending(false);
      }
    },
    [floors, createFloor, setSeats, t],
  );

  return { commit, pending };
}
