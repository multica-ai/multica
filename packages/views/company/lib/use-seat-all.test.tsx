/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { CompanyFloor } from "@multica/core/company";
import type { Squad } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { useSeatAll } from "./use-seat-all";

const api = vi.hoisted(() => ({
  createCompanyFloor: vi.fn(),
  setCompanySeats: vi.fn(),
}));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: api,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

const FLOORS: CompanyFloor[] = [{ id: "f1", workspace_id: "ws-1", name: "Floor 1", position: 0, created_at: "", updated_at: "" }];

function squad(id: string, seat?: number): Squad {
  return { id, name: id, archived_at: null, floor_id: seat === undefined ? null : "f1", company_slot: seat ?? null } as Squad;
}

function Harness({ squads }: { squads: Squad[] }) {
  const { seatAll } = useSeatAll(FLOORS, squads);
  return (
    <button type="button" onClick={seatAll}>
      go
    </button>
  );
}

describe("useSeatAll", () => {
  beforeEach(() => {
    api.createCompanyFloor.mockReset();
    api.setCompanySeats.mockReset().mockResolvedValue({ squads: [] });
  });
  afterEach(cleanup);

  function run(squads: Squad[]) {
    renderWithI18n(
      <QueryClientProvider client={new QueryClient()}>
        <Harness squads={squads} />
      </QueryClientProvider>,
    );
    fireEvent.click(screen.getByRole("button", { name: "go" }));
  }

  it("writes every seat in one request", async () => {
    run([squad("kept", 0), squad("a"), squad("b")]);

    await waitFor(() => expect(api.setCompanySeats).toHaveBeenCalledTimes(1));
    expect(api.setCompanySeats).toHaveBeenCalledWith([
      { squad_id: "a", floor_id: "f1", slot: 1 },
      { squad_id: "b", floor_id: "f1", slot: 2 },
    ]);
    expect(api.createCompanyFloor).not.toHaveBeenCalled();
  });

  it("adds a floor first when the existing ones are full", async () => {
    api.createCompanyFloor.mockResolvedValue({ id: "f2", name: "Floor 2" });
    const full = Array.from({ length: 6 }, (_, slot) => squad(`k${slot}`, slot));
    run([...full, squad("extra")]);

    await waitFor(() => expect(api.setCompanySeats).toHaveBeenCalledTimes(1));
    expect(api.createCompanyFloor).toHaveBeenCalledWith({ name: "Floor 2" });
    expect(api.setCompanySeats).toHaveBeenCalledWith([{ squad_id: "extra", floor_id: "f2", slot: 0 }]);
  });

  it("writes nothing when every squad already has a room", () => {
    run([squad("a", 0)]);

    expect(api.setCompanySeats).not.toHaveBeenCalled();
  });
});
