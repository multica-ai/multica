import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "../api/client";
import { emptySquadCharter, parseCharterRevisionConflict } from "./charter-schema";

afterEach(() => {
  vi.unstubAllGlobals();
});

function respond(body: unknown, status = 200) {
  const fetchMock = vi.fn().mockResolvedValue(
    new Response(body === undefined ? null : JSON.stringify(body), {
      status,
      headers: { "Content-Type": "application/json" },
    }),
  );
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

const charter = {
  squad_id: "s1",
  mission: "Find gaps",
  responsibilities: [{ key: "r1", text: "Track" }],
  outputs: [],
  boundaries: "",
  escalation: "",
  intake: "",
  response_hours: 48,
  operating_review_enabled: true,
  revision: 2,
  updated_by_type: "member",
  updated_by_id: null,
  updated_at: null,
};

const routine = {
  autopilot_id: "ap1",
  squad_id: "s1",
  autopilot_title: "Scan",
  autopilot_status: "active",
  purpose: "Scan",
  expected_output: "none",
  required: true,
  grace_hours: 24,
  state: "never_ran",
  schedule: null,
  next_run_at: null,
  last_run: null,
  created_at: "t",
};

const saveRequest = {
  revision: 2,
  mission: "Find gaps",
  responsibilities: [{ key: "r1", text: "Track" }],
  outputs: [],
  boundaries: "",
  escalation: "",
  intake: "",
  response_hours: 48,
};

describe("ApiClient charter endpoints", () => {
  it("GET /api/squads/:id/charter parses the charter and falls back to revision 0", async () => {
    respond(charter);
    const client = new ApiClient("https://api.test");
    expect(await client.getSquadCharter("s1")).toMatchObject({ mission: "Find gaps", revision: 2 });

    respond({ revision: "two" });
    expect(await client.getSquadCharter("s1")).toEqual(emptySquadCharter("s1"));
  });

  it("PUT /api/squads/:id/charter sends the revision and fields, and reads the saved charter", async () => {
    const fetchMock = respond({ ...charter, revision: 3 });
    const client = new ApiClient("https://api.test");
    const saved = await client.updateSquadCharter("s1", { ...saveRequest, operating_review_enabled: false });

    expect(saved.revision).toBe(3);
    expect(fetchMock.mock.calls[0]?.[0]).toBe("https://api.test/api/squads/s1/charter");
    expect(fetchMock.mock.calls[0]?.[1]?.method).toBe("PUT");
    expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toMatchObject({
      revision: 2,
      operating_review_enabled: false,
    });
  });

  it("PUT exposes the current revision on a 409 conflict", async () => {
    respond({ error: "the charter changed", code: "charter_revision_conflict", revision: 7 }, 409);
    const err = await new ApiClient("https://api.test")
      .updateSquadCharter("s1", saveRequest)
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).status).toBe(409);
    expect(parseCharterRevisionConflict((err as ApiError).body)).toBe(7);
  });

  it("GET /api/squads/:id/routines parses the list and tolerates a malformed body", async () => {
    respond([routine]);
    const client = new ApiClient("https://api.test");
    expect(await client.listSquadRoutines("s1")).toHaveLength(1);

    respond({ routines: [] });
    expect(await client.listSquadRoutines("s1")).toEqual([]);

    respond([{ purpose: "no id" }]);
    expect(await client.listSquadRoutines("s1")).toEqual([]);
  });

  it("POST /api/squads/:id/routines sends the cadence and returns the routine", async () => {
    const fetchMock = respond({ ...routine, state: "on_time" }, 201);
    const client = new ApiClient("https://api.test");
    const created = await client.createSquadRoutine("s1", { cadence: "weekly", purpose: "Scan", expected_output: "report" });

    expect(created.state).toBe("on_time");
    expect(fetchMock.mock.calls[0]?.[1]?.method).toBe("POST");
    expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toEqual({
      cadence: "weekly",
      purpose: "Scan",
      expected_output: "report",
    });
  });

  it("DELETE /api/squads/:id/routines/:routine unlinks, and pauses only when asked", async () => {
    const fetchMock = respond(undefined, 204);
    const client = new ApiClient("https://api.test");
    await client.deleteSquadRoutine("s1", "ap1", { pause: false });
    await client.deleteSquadRoutine("s1", "ap1", { pause: true });

    expect(fetchMock.mock.calls[0]?.[0]).toBe("https://api.test/api/squads/s1/routines/ap1");
    expect(fetchMock.mock.calls[1]?.[0]).toBe("https://api.test/api/squads/s1/routines/ap1?pause=true");
    expect(fetchMock.mock.calls[0]?.[1]?.method).toBe("DELETE");
  });
});
