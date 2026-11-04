/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { OfficeFloor } from "@multica/core/office";
import type { Agent, Squad } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { OfficeSeatBanner, OfficeUnseatedTray } from "./office-unseated-tray";

const seats = vi.hoisted(() => ({ mutate: vi.fn() }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/office", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/office")>()),
  useSetOfficeSeats: () => ({ mutate: seats.mutate }),
}));
// The agent menu has its own test; here it only reports which agent it belongs to.
vi.mock("./office-home-squad-menu", () => ({
  OfficeHomeSquadMenu: ({ agent, trigger }: { agent: Agent; trigger: React.ReactElement }) => (
    <span data-testid={`home-menu-${agent.id}`}>{trigger}</span>
  ),
}));

const FLOORS: OfficeFloor[] = [
  { id: "f1", workspace_id: "ws-1", name: "Research", position: 0, created_at: "", updated_at: "" },
  { id: "f2", workspace_id: "ws-1", name: "Build", position: 1, created_at: "", updated_at: "" },
];

function squad(id: string, seat?: { floor: string; slot: number }): Squad {
  return { id, name: `Squad ${id}`, archived_at: null, floor_id: seat?.floor ?? null, office_slot: seat?.slot ?? null } as Squad;
}

describe("OfficeUnseatedTray", () => {
  beforeEach(() => seats.mutate.mockReset());
  afterEach(cleanup);

  function setup(props: Partial<Parameters<typeof OfficeUnseatedTray>[0]> = {}) {
    const loose = squad("loose");
    return renderWithI18n(
      <QueryClientProvider client={new QueryClient()}>
        <OfficeUnseatedTray
          squads={[loose]}
          agents={[{ id: "ada", name: "Ada" } as Agent]}
          floors={FLOORS}
          allSquads={[loose, squad("a", { floor: "f1", slot: 0 })]}
          canSeat={() => true}
          {...props}
        />
      </QueryClientProvider>,
    );
  }

  it("is collapsed until opened, and says how many are outside the floors", () => {
    setup();

    const toggle = screen.getByRole("button", { name: /Not on a floor/ });
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    expect(screen.queryByRole("button", { name: /Squad loose/ })).toBeNull();

    fireEvent.click(toggle);

    expect(screen.getByRole("button", { name: /Squad loose/ })).toBeTruthy();
    expect(screen.getByTestId("home-menu-ada")).toBeTruthy();
  });

  it("renders nothing when every squad and agent is placed", () => {
    setup({ squads: [], agents: [] });

    expect(screen.queryByRole("region")).toBeNull();
  });

  it("seats a squad in a free room of a floor", async () => {
    // The submenu opens under a pointer-events guard that jsdom never lifts.
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    setup();
    fireEvent.click(screen.getByRole("button", { name: /Not on a floor/ }));

    await user.click(screen.getByRole("button", { name: "Squad loose: seat in a room" }));
    await user.click(await screen.findByRole("menuitem", { name: "Research" }));
    // Room 1 of Research is taken, so the first free one is Room 2.
    expect(screen.queryByRole("menuitem", { name: "Room 1" })).toBeNull();
    fireEvent.click(await screen.findByRole("menuitem", { name: "Room 2" }));

    expect(seats.mutate).toHaveBeenCalledWith(
      [{ squad_id: "loose", floor_id: "f1", slot: 1 }],
      expect.any(Object),
    );
  });

  it("shows a squad the viewer cannot move as a plain chip", () => {
    setup({ canSeat: () => false });
    fireEvent.click(screen.getByRole("button", { name: /Not on a floor/ }));

    expect(screen.getByText("Squad loose")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Squad loose: seat in a room" })).toBeNull();
  });
});

describe("OfficeSeatBanner", () => {
  afterEach(cleanup);

  it("counts the squads without a room and seats them all on request", () => {
    const onSeatAll = vi.fn();
    renderWithI18n(<OfficeSeatBanner count={3} pending={false} onSeatAll={onSeatAll} />);

    expect(screen.getByText("3 squads are outside the company")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Add all to company" }));

    expect(onSeatAll).toHaveBeenCalled();
  });

  it("is disabled while seating", () => {
    renderWithI18n(<OfficeSeatBanner count={1} pending onSeatAll={() => {}} />);

    expect((screen.getByRole("button", { name: "Add all to company" }) as HTMLButtonElement).disabled).toBe(true);
  });
});
