"use client";

import { FolderKanban } from "@multica/ui/icons";
import { useT } from "../../i18n";
import type { OfficeStage } from "./office-canvas";

/**
 * Each whiteboard on the floor as a button that opens the kanban board. The
 * button sits exactly over the board the engine paints, so the drawing is the
 * control, and a caption above it says what a click does.
 */
export function OfficeBoardButton({ stage, onOpen }: { stage: OfficeStage; onOpen: () => void }) {
  const { t } = useT("issues");
  const { units, boards } = stage;
  return (
    <>
      {boards.map((board, index) => (
        <button
          key={index}
          type="button"
          aria-label={t(($) => $.office.board_open)}
          onClick={onOpen}
          className="group pointer-events-auto absolute cursor-pointer rounded-[2px] ring-2 ring-white/50 transition hover:bg-white/15 hover:ring-white focus-visible:ring-white"
          style={{
            left: `${(board.x / units.w) * 100}%`,
            top: `${(board.y / units.h) * 100}%`,
            width: `${(board.w / units.w) * 100}%`,
            height: `${(board.h / units.h) * 100}%`,
          }}
        >
          <span
            aria-hidden
            className="absolute bottom-full left-1/2 mb-1 flex -translate-x-1/2 items-center gap-1 whitespace-nowrap rounded-[3px] bg-[#2c2418]/90 px-2 py-0.5 text-micro font-semibold text-[#fff8ea] shadow transition group-hover:bg-[#1d5fd6]"
          >
            <FolderKanban className="size-3" />
            {t(($) => $.office.board_label)}
          </span>
        </button>
      ))}
    </>
  );
}
