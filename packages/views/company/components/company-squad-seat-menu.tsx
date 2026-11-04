"use client";

import type { ReactElement, ReactNode } from "react";
import { toast } from "sonner";
import { ApiError } from "@multica/core/api";
import { freeSlots, parseCompanySeatConflict, useSetCompanySeats, type CompanyFloor } from "@multica/core/company";
import type { Squad } from "@multica/core/types";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useT } from "../../i18n";

/**
 * A menu that seats a squad in a room of any floor, or moves a seated squad to another floor or
 * room. The server moves it in one request, so the menu lists every free room and a seated squad
 * can be picked again.
 */
export function CompanySquadSeatMenu({
  squad,
  floors,
  allSquads,
  trigger,
  children,
}: {
  squad: Squad;
  floors: readonly CompanyFloor[];
  allSquads: readonly Squad[];
  /** The element that opens the menu; it takes the menu's trigger props. */
  trigger: ReactElement;
  children: ReactNode;
}) {
  const { t } = useT("issues");
  const setSeats = useSetCompanySeats();

  const seat = (floorId: string, slot: number) =>
    setSeats.mutate([{ squad_id: squad.id, floor_id: floorId, slot }], {
      onSuccess: () => toast.success(t(($) => $.company.seat_done, { name: squad.name })),
      onError: (error) => {
        const holder = error instanceof ApiError ? parseCompanySeatConflict(error.body) : null;
        toast.error(holder ? t(($) => $.company.seat_taken, { name: holder.squad_name }) : t(($) => $.company.seat_failed));
      },
    });

  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={trigger}>{children}</DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-48">
        {floors.map((floor) => {
          const slots = freeSlots(floor.id, allSquads);
          return (
            <DropdownMenuSub key={floor.id}>
              <DropdownMenuSubTrigger disabled={slots.length === 0}>{floor.name}</DropdownMenuSubTrigger>
              <DropdownMenuSubContent>
                {slots.map((slot) => (
                  <DropdownMenuItem key={slot} onClick={() => seat(floor.id, slot)}>
                    {t(($) => $.company.seat_room, { n: slot + 1 })}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuSubContent>
            </DropdownMenuSub>
          );
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
