"use client";

import { Building2, ChevronDown } from "@multica/ui/icons";
import { OFFICE_ROOMS, OFFICE_ROOM_CAPACITY, type OfficeRoom } from "@multica/core/agents";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useT } from "../../i18n";

/**
 * Where the agent sits, and the way to move it. A room with no desk left, or
 * the one the agent is in, cannot be chosen; a choice moves the agent at once.
 */
export function OfficeRoomMenu({
  current,
  occupancy,
  roomNames,
  disabled,
  onMove,
}: {
  current: OfficeRoom;
  /** How many agents on the floor sit in each room. */
  occupancy: Record<OfficeRoom, number>;
  roomNames: Record<OfficeRoom, string>;
  disabled?: boolean;
  onMove: (room: OfficeRoom) => void;
}) {
  const { t } = useT("issues");
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={<Button type="button" variant="outline" size="sm" disabled={disabled} aria-label={t(($) => $.office.room_label)} />}
      >
        <Building2 className="size-4" />
        {roomNames[current]}
        <ChevronDown className="size-3.5 text-muted-foreground" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56">
        {OFFICE_ROOMS.map((room) => {
          const count = occupancy[room];
          const full = count >= OFFICE_ROOM_CAPACITY;
          const here = room === current;
          return (
            <DropdownMenuItem key={room} disabled={here || full} onClick={() => onMove(room)}>
              <span className="min-w-0 flex-1 truncate">{roomNames[room]}</span>
              <span className="text-caption text-muted-foreground">
                {here
                  ? t(($) => $.office.room_current)
                  : full
                    ? t(($) => $.office.room_full)
                    : t(($) => $.office.room_seats, { count, capacity: OFFICE_ROOM_CAPACITY })}
              </span>
            </DropdownMenuItem>
          );
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
