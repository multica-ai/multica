"use client";

import { Plus } from "@multica/ui/icons";
import type { FloorRoom } from "@multica/core/office";
import { useT } from "../../i18n";
import type { OfficeStage } from "./office-canvas";

/**
 * Controls pinned over the rooms the engine reports, by slot: "Create squad" in
 * an empty room, the `+N` chip of a room whose squad has more agents than
 * desks, and a count of the proposals waiting in it. Each sits at its room's
 * own rectangle, in the stage's layout units.
 */
export function OfficeRoomOverlays({
  stage,
  rooms,
  pendingBySlot,
  onCreateSquad,
  onOpenSquad,
  onOpenDecisions,
}: {
  stage: OfficeStage;
  /** The floor's rooms, indexed by slot; the stage's rooms are in the same order. */
  rooms: readonly FloorRoom[];
  /** Pending proposals per slot. */
  pendingBySlot: readonly number[];
  onCreateSquad: () => void;
  onOpenSquad: (slot: number) => void;
  onOpenDecisions: () => void;
}) {
  const { t } = useT("issues");
  const { units } = stage;
  return (
    <>
      {stage.rooms.map((rect, index) => {
        const room = rooms[index];
        if (!room) return null;
        const box = {
          left: `${(rect.x / units.w) * 100}%`,
          top: `${(rect.y / units.h) * 100}%`,
          width: `${(rect.w / units.w) * 100}%`,
          height: `${(rect.h / units.h) * 100}%`,
        };
        const pending = pendingBySlot[index] ?? 0;
        return (
          <div key={index} className="absolute" style={box}>
            {pending > 0 ? (
              <button
                type="button"
                aria-label={t(($) => $.office.decision_badge, { count: pending })}
                onClick={onOpenDecisions}
                className="pointer-events-auto absolute -right-2 -top-2 flex min-w-5 cursor-pointer items-center justify-center rounded-full bg-[#1d5fd6] px-1.5 py-0.5 text-micro font-semibold text-white shadow hover:bg-[#174db0]"
              >
                {pending}
              </button>
            ) : null}
            {!room.squad ? (
              <button
                type="button"
                onClick={onCreateSquad}
                className="pointer-events-auto absolute left-1/2 top-1/2 flex -translate-x-1/2 -translate-y-1/2 cursor-pointer items-center gap-1 whitespace-nowrap rounded-[3px] bg-[#2c2418]/90 px-2 py-1 text-micro font-semibold text-[#fff8ea] shadow hover:bg-[#1d5fd6]"
              >
                <Plus className="size-3" />
                {t(($) => $.office.create_squad)}
              </button>
            ) : null}
            {room.overflow > 0 ? (
              <button
                type="button"
                aria-label={t(($) => $.office.overflow_label, { count: room.overflow })}
                onClick={() => onOpenSquad(index)}
                className="pointer-events-auto absolute bottom-0 right-0 cursor-pointer rounded-[3px] bg-[#2c2418]/90 px-1.5 py-0.5 text-micro font-semibold text-[#fff8ea] shadow hover:bg-[#1d5fd6]"
              >
                {`+${room.overflow}`}
              </button>
            ) : null}
          </div>
        );
      })}
    </>
  );
}
