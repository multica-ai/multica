import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "../api/client";
import { SquadListSchema, SquadSchema } from "../api/schemas";
import { parseWithFallback } from "../api/schema";
import {
  EMPTY_OFFICE_FLOOR,
  OfficeFloorListSchema,
  OfficeFloorSchema,
  OfficeSeatsResponseSchema,
  parseOfficeSeatConflict,
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

describe("office floor schemas", () => {
  it("defaults the optional fields", () => {
    expect(OfficeFloorSchema.parse({ id: "f", name: "X" })).toMatchObject({ position: 0, workspace_id: "", created_at: "" });
  });

  it("keeps unknown fields from newer servers", () => {
    expect(OfficeFloorSchema.parse({ ...floor, theme: "dark" })).toMatchObject({ theme: "dark" });
  });

  it("rejects a floor with no id or a non-string name", () => {
    expect(OfficeFloorSchema.safeParse({ name: "X" }).success).toBe(false);
    expect(OfficeFloorSchema.safeParse({ id: "f", name: 3 }).success).toBe(false);
    expect(OfficeFloorListSchema.safeParse({ floors: [] }).success).toBe(false);
  });
});

describe("squad schema office seat", () => {
  it("defaults the seat to null for older servers", () => {
    const parsed = SquadSchema.parse(squad);
    expect(parsed.floor_id).toBeNull();
    expect(parsed.office_slot).toBeNull();
  });

  it("reads the seat", () => {
    const parsed = SquadListSchema.parse([{ ...squad, floor_id: "f1", office_slot: 4 }]);
    expect(parsed[0]).toMatchObject({ floor_id: "f1", office_slot: 4 });
  });

  it("rejects a non-numeric slot", () => {
    expect(SquadSchema.safeParse({ ...squad, office_slot: "four" }).success).toBe(false);
  });
});

describe("seat conflict body", () => {
  const body = {
    error: "seat is already taken",
    code: "seat_conflict",
    conflict: { floor_id: "f1", slot: 2, squad_id: "s2", squad_name: "Holder" },
  };

  it("names the holder", () => {
    expect(parseOfficeSeatConflict(body)).toEqual({ floor_id: "f1", slot: 2, squad_id: "s2", squad_name: "Holder" });
  });

  it("returns null for other errors and malformed bodies", () => {
    expect(parseOfficeSeatConflict({ error: "x", code: "floor_limit" })).toBeNull();
    expect(parseOfficeSeatConflict({ code: "seat_conflict" })).toBeNull();
    expect(parseOfficeSeatConflict({ ...body, conflict: { floor_id: "f1" } })).toBeNull();
    expect(parseOfficeSeatConflict(null)).toBeNull();
    expect(parseOfficeSeatConflict("nope")).toBeNull();
  });
});

describe("ApiClient office endpoints", () => {
  it("GET /api/office/floors parses floors and tolerates a malformed body", async () => {
    respond([floor]);
    expect(await new ApiClient("https://api.test").listOfficeFloors()).toEqual([floor]);

    respond({ floors: "nope" });
    expect(await new ApiClient("https://api.test").listOfficeFloors()).toEqual([]);

    respond([{ id: 1 }]);
    expect(await new ApiClient("https://api.test").listOfficeFloors()).toEqual([]);

    respond(null);
    expect(await new ApiClient("https://api.test").listOfficeFloors()).toEqual([]);
  });

  it("POST /api/office/floors sends the name and falls back to an empty floor", async () => {
    const fetchMock = respond(floor, 201);
    const client = new ApiClient("https://api.test");
    expect(await client.createOfficeFloor({ name: "Research" })).toEqual(floor);
    expect(fetchMock.mock.calls[0]?.[0]).toBe("https://api.test/api/office/floors");
    expect(fetchMock.mock.calls[0]?.[1]?.method).toBe("POST");
    expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toEqual({ name: "Research" });

    respond("garbage", 201);
    expect(await client.createOfficeFloor({ name: "x" })).toEqual(EMPTY_OFFICE_FLOOR);
  });

  it("PUT /api/office/floors/:id sends only the given fields and falls back", async () => {
    const fetchMock = respond({ ...floor, position: 2 });
    const client = new ApiClient("https://api.test");
    expect((await client.updateOfficeFloor("f1", { position: 2 })).position).toBe(2);
    expect(fetchMock.mock.calls[0]?.[0]).toBe("https://api.test/api/office/floors/f1");
    expect(fetchMock.mock.calls[0]?.[1]?.method).toBe("PUT");
    expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toEqual({ position: 2 });

    respond({ name: 5 });
    expect(await client.updateOfficeFloor("f1", { name: "x" })).toEqual(EMPTY_OFFICE_FLOOR);
  });

  it("DELETE /api/office/floors/:id surfaces a refusal with its code", async () => {
    respond({ error: "move or unseat the squads", code: "floor_not_empty" }, 409);
    const err = await new ApiClient("https://api.test").deleteOfficeFloor("f1").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).status).toBe(409);
    expect((err as ApiError).body).toMatchObject({ code: "floor_not_empty" });
  });

  it("PUT /api/office/seats sends the array, parses squads and tolerates a malformed body", async () => {
    const fetchMock = respond({ squads: [{ ...squad, floor_id: "f1", office_slot: 1 }] });
    const client = new ApiClient("https://api.test");
    const seats = [{ squad_id: "s1", floor_id: "f1", slot: 1 }, { squad_id: "s2", floor_id: null, slot: null }];
    const result = await client.setOfficeSeats(seats);
    expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toEqual(seats);
    expect(result.squads[0]).toMatchObject({ id: "s1", floor_id: "f1", office_slot: 1 });

    respond({ squads: "nope" });
    expect((await client.setOfficeSeats(seats)).squads).toEqual([]);

    respond({});
    expect((await client.setOfficeSeats(seats)).squads).toEqual([]);
  });

  it("PUT /api/office/seats exposes the holder on a 409", async () => {
    const conflict = {
      error: "seat is already taken",
      code: "seat_conflict",
      conflict: { floor_id: "f1", slot: 0, squad_id: "s9", squad_name: "Holder" },
    };
    respond(conflict, 409);
    const err = await new ApiClient("https://api.test")
      .setOfficeSeats([{ squad_id: "s1", floor_id: "f1", slot: 0 }])
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(parseOfficeSeatConflict((err as ApiError).body)?.squad_name).toBe("Holder");
  });

  it("parseWithFallback keeps the empty seats response for a malformed body", () => {
    const fallback = { squads: [] };
    expect(parseWithFallback({ squads: [{ id: 1 }] }, OfficeSeatsResponseSchema, fallback, { endpoint: "t" })).toBe(fallback);
  });
});
