"use client";

import { useCallback, useState } from "react";
import { toast } from "sonner";
import {
  planSeatAll,
  useCreateOfficeFloor,
  useSetOfficeSeats,
  type OfficeFloor,
} from "@multica/core/office";
import type { Squad } from "@multica/core/types";
import { useT } from "../../i18n";

/**
 * "Seat all": puts every unseated squad in the next free room, adding floors
 * once the existing ones are full. Floors are created first (each awaited, as
 * the server can refuse), then every seat is written in one request.
 */
export function useSeatAll(floors: readonly OfficeFloor[], squads: readonly Squad[]) {
  const { t } = useT("issues");
  const createFloor = useCreateOfficeFloor();
  const setSeats = useSetOfficeSeats();
  const [pending, setPending] = useState(false);

  const seatAll = useCallback(async () => {
    const plan = planSeatAll(floors, squads);
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
          name = t(($) => $.office.floor_default_name, { n });
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
      toast.success(t(($) => $.office.seat_all_done, { count: plan.entries.length }));
      if (plan.skipped > 0) toast.error(t(($) => $.office.seat_all_skipped, { count: plan.skipped }));
    } catch {
      toast.error(t(($) => $.office.seat_failed));
    } finally {
      setPending(false);
    }
  }, [floors, squads, createFloor, setSeats, t]);

  return { seatAll, pending };
}
