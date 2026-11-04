/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, renderHook, act } from "@testing-library/react";
import type { CompanyFloor } from "@multica/core/company";
import type { Squad } from "@multica/core/types";
import { useAddSquadToCompany } from "./use-add-squad-to-company";

const seats = vi.hoisted(() => ({ mutateAsync: vi.fn(), createAsync: vi.fn() }));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (selector: (dict: Record<string, unknown>) => unknown, options?: { n?: number }) =>
      String(selector({ company: { floor_default_name: "Floor {{n}}" } })).replace("{{n}}", String(options?.n ?? "")),
  }),
}));
vi.mock("@multica/core/company", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/company")>()),
  useSetCompanySeats: () => ({ mutateAsync: seats.mutateAsync }),
  useCreateCompanyFloor: () => ({ mutateAsync: seats.createAsync }),
}));

const FLOOR: CompanyFloor = { id: "f1", workspace_id: "ws", name: "Floor 1", position: 0, created_at: "", updated_at: "" };

function squad(id: string, seat?: { floor: string; slot: number }): Squad {
  return {
    id,
    name: `Squad ${id}`,
    archived_at: null,
    floor_id: seat?.floor ?? null,
    company_slot: seat?.slot ?? null,
  } as Squad;
}

describe("useAddSquadToCompany", () => {
  beforeEach(() => {
    seats.mutateAsync.mockReset().mockResolvedValue([]);
    seats.createAsync.mockReset().mockResolvedValue({ id: "new-floor" });
  });
  afterEach(cleanup);

  it("seats a squad outside the company in the first free room, without touching the seated squads", async () => {
    const outside = squad("outside");
    const seated = squad("seated", { floor: "f1", slot: 0 });
    const { result } = renderHook(() => useAddSquadToCompany([FLOOR], [seated, outside]));

    await act(async () => {
      await result.current.addSquad(outside);
    });

    expect(seats.createAsync).not.toHaveBeenCalled();
    expect(seats.mutateAsync).toHaveBeenCalledWith([{ squad_id: "outside", floor_id: "f1", slot: 1 }]);
  });

  it("creates a floor when every existing floor's rooms are taken", async () => {
    const outside = squad("outside");
    const seatedAll = Array.from({ length: 6 }, (_, slot) => squad(`s${slot}`, { floor: "f1", slot }));
    const { result } = renderHook(() => useAddSquadToCompany([FLOOR], [...seatedAll, outside]));

    await act(async () => {
      await result.current.addSquad(outside);
    });

    expect(seats.createAsync).toHaveBeenCalledWith({ name: "Floor 2" });
    expect(seats.mutateAsync).toHaveBeenCalledWith([{ squad_id: "outside", floor_id: "new-floor", slot: 0 }]);
  });
});
