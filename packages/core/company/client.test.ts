import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "../api/client";
import { SquadListSchema, SquadSchema } from "../api/schemas";
import { parseWithFallback } from "../api/schema";
import {
  EMPTY_COMPANY_FLOOR,
  CompanyFloorListSchema,
  CompanyFloorSchema,
  CompanySeatsResponseSchema,
  parseCompanySeatConflict,
} from "./schemas";

afterEach(() => {
  vi.unstubAllGlobals();
});

function respond(body: unknown, status = 200) {
  const fetchMock = vi.fn().mockResolvedValue(
    new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }),
  );
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

const floor = { id: "f1", workspace_id: "ws", name: "Research", position: 0, created_at: "t", updated_at: "t" };
const squad = {
  id: "s1",
  workspace_id: "ws",
  name: "Squad",
  leader_id: "a1",
  creator_id: "u1",
  created_at: "t",
  updated_at: "t",
};

describe("proposal revisions", () => {
  const revision = {
    revision: 1,
    body: "First filing",
    created_by_type: "agent",
    created_by_id: "a1",
    created_at: "2026-11-01T00:00:00Z",
  };

  it("GET /api/proposals/:id/revisions parses the history and tolerates a malformed body", async () => {
    const fetchMock = respond([revision]);
    expect(await new ApiClient("https://api.test").listProposalRevisions("p1")).toEqual([revision]);
    expect(fetchMock.mock.calls[0]?.[0]).toBe("https://api.test/api/proposals/p1/revisions");

    respond({ revisions: [revision] });
    expect(await new ApiClient("https://api.test").listProposalRevisions("p1")).toEqual([]);
  });
});

describe("company floor schemas", () => {
  it("defaults the optional fields", () => {
    expect(CompanyFloorSchema.parse({ id: "f", name: "X" })).toMatchObject({ position: 0, workspace_id: "", created_at: "" });
  });

  it("keeps unknown fields from newer servers", () => {
    expect(CompanyFloorSchema.parse({ ...floor, theme: "dark" })).toMatchObject({ theme: "dark" });
  });

  it("rejects a floor with no id or a non-string name", () => {
    expect(CompanyFloorSchema.safeParse({ name: "X" }).success).toBe(false);
    expect(CompanyFloorSchema.safeParse({ id: "f", name: 3 }).success).toBe(false);
    expect(CompanyFloorListSchema.safeParse({ floors: [] }).success).toBe(false);
  });
});

describe("squad schema company seat", () => {
  it("defaults the seat to null for older servers", () => {
    const parsed = SquadSchema.parse(squad);
    expect(parsed.floor_id).toBeNull();
    expect(parsed.company_slot).toBeNull();
  });

  it("reads the seat", () => {
    const parsed = SquadListSchema.parse([{ ...squad, floor_id: "f1", company_slot: 4 }]);
    expect(parsed[0]).toMatchObject({ floor_id: "f1", company_slot: 4 });
  });

  it("rejects a non-numeric slot", () => {
    expect(SquadSchema.safeParse({ ...squad, company_slot: "four" }).success).toBe(false);
  });
});

describe("seat conflict body", () => {
  const body = {
    error: "seat is already taken",
    code: "seat_conflict",
    conflict: { floor_id: "f1", slot: 2, squad_id: "s2", squad_name: "Holder" },
  };

  it("names the holder", () => {
    expect(parseCompanySeatConflict(body)).toEqual({ floor_id: "f1", slot: 2, squad_id: "s2", squad_name: "Holder" });
  });

  it("returns null for other errors and malformed bodies", () => {
    expect(parseCompanySeatConflict({ error: "x", code: "floor_limit" })).toBeNull();
    expect(parseCompanySeatConflict({ code: "seat_conflict" })).toBeNull();
    expect(parseCompanySeatConflict({ ...body, conflict: { floor_id: "f1" } })).toBeNull();
    expect(parseCompanySeatConflict(null)).toBeNull();
    expect(parseCompanySeatConflict("nope")).toBeNull();
  });
});

describe("ApiClient company endpoints", () => {
  it("GET /api/company/floors parses floors and tolerates a malformed body", async () => {
    respond([floor]);
    expect(await new ApiClient("https://api.test").listCompanyFloors()).toEqual([floor]);

    respond({ floors: "nope" });
    expect(await new ApiClient("https://api.test").listCompanyFloors()).toEqual([]);

    respond([{ id: 1 }]);
    expect(await new ApiClient("https://api.test").listCompanyFloors()).toEqual([]);

    respond(null);
    expect(await new ApiClient("https://api.test").listCompanyFloors()).toEqual([]);
  });

  it("POST /api/company/floors sends the name and falls back to an empty floor", async () => {
    const fetchMock = respond(floor, 201);
    const client = new ApiClient("https://api.test");
    expect(await client.createCompanyFloor({ name: "Research" })).toEqual(floor);
    expect(fetchMock.mock.calls[0]?.[0]).toBe("https://api.test/api/company/floors");
    expect(fetchMock.mock.calls[0]?.[1]?.method).toBe("POST");
    expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toEqual({ name: "Research" });

    respond("garbage", 201);
    expect(await client.createCompanyFloor({ name: "x" })).toEqual(EMPTY_COMPANY_FLOOR);
  });

  it("PUT /api/company/floors/:id sends only the given fields and falls back", async () => {
    const fetchMock = respond({ ...floor, position: 2 });
    const client = new ApiClient("https://api.test");
    expect((await client.updateCompanyFloor("f1", { position: 2 })).position).toBe(2);
    expect(fetchMock.mock.calls[0]?.[0]).toBe("https://api.test/api/company/floors/f1");
    expect(fetchMock.mock.calls[0]?.[1]?.method).toBe("PUT");
    expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toEqual({ position: 2 });

    respond({ name: 5 });
    expect(await client.updateCompanyFloor("f1", { name: "x" })).toEqual(EMPTY_COMPANY_FLOOR);
  });

  it("DELETE /api/company/floors/:id surfaces a refusal with its code", async () => {
    respond({ error: "move or unseat the squads", code: "floor_not_empty" }, 409);
    const err = await new ApiClient("https://api.test").deleteCompanyFloor("f1").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).status).toBe(409);
    expect((err as ApiError).body).toMatchObject({ code: "floor_not_empty" });
  });

  it("PUT /api/company/seats sends the array, parses squads and tolerates a malformed body", async () => {
    const fetchMock = respond({ squads: [{ ...squad, floor_id: "f1", company_slot: 1 }] });
    const client = new ApiClient("https://api.test");
    const seats = [{ squad_id: "s1", floor_id: "f1", slot: 1 }, { squad_id: "s2", floor_id: null, slot: null }];
    const result = await client.setCompanySeats(seats);
    expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toEqual(seats);
    expect(result.squads[0]).toMatchObject({ id: "s1", floor_id: "f1", company_slot: 1 });

    respond({ squads: "nope" });
    expect((await client.setCompanySeats(seats)).squads).toEqual([]);

    respond({});
    expect((await client.setCompanySeats(seats)).squads).toEqual([]);
  });

  it("PUT /api/company/seats exposes the holder on a 409", async () => {
    const conflict = {
      error: "seat is already taken",
      code: "seat_conflict",
      conflict: { floor_id: "f1", slot: 0, squad_id: "s9", squad_name: "Holder" },
    };
    respond(conflict, 409);
    const err = await new ApiClient("https://api.test")
      .setCompanySeats([{ squad_id: "s1", floor_id: "f1", slot: 0 }])
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(parseCompanySeatConflict((err as ApiError).body)?.squad_name).toBe("Holder");
  });

  it("parseWithFallback keeps the empty seats response for a malformed body", () => {
    const fallback = { squads: [] };
    expect(parseWithFallback({ squads: [{ id: 1 }] }, CompanySeatsResponseSchema, fallback, { endpoint: "t" })).toBe(fallback);
  });
});
