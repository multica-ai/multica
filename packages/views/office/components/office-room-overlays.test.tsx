/**
 * @vitest-environment jsdom
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import type { FloorRoom } from "@multica/core/office";
import type { Squad } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { OfficeRoomOverlays } from "./office-room-overlays";
import type { OfficeStage } from "./office-canvas";

const STAGE: OfficeStage = {
  units: { w: 100, h: 100 },
  tank: null,
  rooms: [
    { x: 0, y: 0, w: 50, h: 50 },
    { x: 50, y: 0, w: 50, h: 50 },
  ],
  boards: [],
};

const ROOMS: FloorRoom[] = [
  { slot: 0, squad: { id: "sq-research", name: "Research", archived_at: null } as Squad, agentIds: [], overflow: 0 },
  { slot: 1, squad: null, agentIds: [], overflow: 0 },
];

function renderOverlays(initiativesBySlot: number[], onOpenPipeline = vi.fn()) {
  renderWithI18n(
    <OfficeRoomOverlays
      stage={STAGE}
      rooms={ROOMS}
      pendingBySlot={[0, 0]}
      initiativesBySlot={initiativesBySlot}
      onCreateSquad={vi.fn()}
      onOpenSquad={vi.fn()}
      onOpenDecisions={vi.fn()}
      onOpenPipeline={onOpenPipeline}
    />,
  );
  return { onOpenPipeline };
}

describe("OfficeRoomOverlays initiative badge", () => {
  afterEach(cleanup);

  it("shows the owned count on a room whose squad owns initiatives", () => {
    renderOverlays([2, 0]);
    expect(screen.getByRole("button", { name: "2 initiatives owned" }).textContent).toBe("2");
  });

  it("shows no badge for a room with no initiatives", () => {
    renderOverlays([0, 0]);
    expect(screen.queryByRole("button", { name: /initiatives? owned/ })).toBeNull();
  });

  it("opens the pipeline filtered to the room's squad", () => {
    const { onOpenPipeline } = renderOverlays([1, 0]);
    fireEvent.click(screen.getByRole("button", { name: "1 initiative owned" }));
    expect(onOpenPipeline).toHaveBeenCalledWith("sq-research");
  });
});
