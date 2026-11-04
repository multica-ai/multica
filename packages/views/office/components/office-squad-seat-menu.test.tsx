/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { OfficeFloor } from "@multica/core/office";
import type { Squad } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { OfficeSquadSeatMenu } from "./office-squad-seat-menu";
import { Button } from "@multica/ui/components/ui/button";

const seats = vi.hoisted(() => ({ mutate: vi.fn() }));

vi.mock("@multica/core/office", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/office")>()),
  useSetOfficeSeats: () => ({ mutate: seats.mutate }),
}));

const FLOORS: OfficeFloor[] = [
  { id: "f1", workspace_id: "ws-1", name: "Development", position: 0, created_at: "", updated_at: "" },
  { id: "f2", workspace_id: "ws-1", name: "Build", position: 1, created_at: "", updated_at: "" },
];

const sdlc = { id: "sdlc", name: "SDLC Greenfield", archived_at: null, floor_id: "f1", office_slot: 0 } as Squad;
const other = { id: "other", name: "Other", archived_at: null, floor_id: "f2", office_slot: 0 } as Squad;

describe("OfficeSquadSeatMenu", () => {
  beforeEach(() => seats.mutate.mockReset());
  afterEach(cleanup);

  function setup() {
    return renderWithI18n(
      <QueryClientProvider client={new QueryClient()}>
        <OfficeSquadSeatMenu
          squad={sdlc}
          floors={FLOORS}
          allSquads={[sdlc, other]}
          trigger={<Button type="button" variant="outline" size="sm" />}
        >
          Move to floor
        </OfficeSquadSeatMenu>
      </QueryClientProvider>,
    );
  }

  it("moves the squad to a free room on the chosen floor in one request", async () => {
    const user = userEvent.setup();
    setup();

    await user.click(screen.getByRole("button", { name: "Move to floor" }));
    await user.click(await screen.findByRole("menuitem", { name: "Build" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Room 2" }));

    expect(seats.mutate).toHaveBeenCalledWith(
      [{ squad_id: "sdlc", floor_id: "f2", slot: 1 }],
      expect.any(Object),
    );
  });
});
