// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { FALLBACK_INITIATIVE } from "./schemas";

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

const initiative = {
  id: "in-1",
  identifier: "INI-1",
  title: "Invoicing for small firms",
  thesis: "Month-end close is slow.",
  pipeline_id: "pl-1",
  stage_key: "define",
  owner_squad_id: "sq-analysis",
  status: "active",
  issue_id: "iss-1",
  build_project_id: null,
  stage_entered_at: "2026-10-20T00:00:00Z",
  days_in_stage: 1,
  sla_state: "ok",
  artifacts_required: 1,
  artifacts_present: 1,
  pending_gate_proposal_id: null,
};

/** The method, path and JSON body the client sent on its one request. */
function sentOf(fetchMock: ReturnType<typeof respond>) {
  const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
  return {
    method: init.method,
    path: url.replace("https://api.test", ""),
    body: init.body === undefined ? undefined : JSON.parse(init.body as string),
  };
}

const client = () => new ApiClient("https://api.test");

describe("initiative create and transition requests", () => {
  it("POST /api/initiatives sends the pipeline, title and thesis", async () => {
    const fetchMock = respond(initiative, 201);
    const created = await client().createInitiative({ pipeline_id: "pl-1", title: "Invoicing", thesis: "Why" });
    expect(sentOf(fetchMock)).toEqual({
      method: "POST",
      path: "/api/initiatives",
      body: { pipeline_id: "pl-1", title: "Invoicing", thesis: "Why" },
    });
    expect(created.id).toBe("in-1");
  });

  it("POST /advance, /attest and /resume send no body", async () => {
    for (const [action, call] of [
      ["advance", (c: ApiClient) => c.advanceInitiative("in-1")],
      ["attest", (c: ApiClient) => c.attestInitiative("in-1")],
      ["resume", (c: ApiClient) => c.resumeInitiative("in-1")],
    ] as const) {
      const fetchMock = respond(initiative);
      await call(client());
      expect(sentOf(fetchMock)).toEqual({ method: "POST", path: `/api/initiatives/in-1/${action}`, body: undefined });
    }
  });

  it("POST /send-back and /shelve send the note", async () => {
    let fetchMock = respond(initiative);
    await client().sendBackInitiative("in-1", "Re-check the price point");
    expect(sentOf(fetchMock)).toEqual({
      method: "POST",
      path: "/api/initiatives/in-1/send-back",
      body: { note: "Re-check the price point" },
    });

    fetchMock = respond(initiative);
    await client().shelveInitiative("in-1", "");
    expect(sentOf(fetchMock)).toEqual({ method: "POST", path: "/api/initiatives/in-1/shelve", body: { note: "" } });
  });

  it("POST /close sends the reason", async () => {
    const fetchMock = respond(initiative);
    await client().closeInitiative("in-1", "Sunset after the 30-day review");
    expect(sentOf(fetchMock)).toEqual({
      method: "POST",
      path: "/api/initiatives/in-1/close",
      body: { reason: "Sunset after the 30-day review" },
    });
  });

  it("reads the initiative a transition returns", async () => {
    respond(initiative);
    expect(await client().shelveInitiative("in-1", "Waiting on legal")).toMatchObject({ id: "in-1", status: "active" });
  });

  it("degrades a malformed transition response to the id-less fallback", async () => {
    for (const bad of ["oops", null, { id: 5 }, { initiative }]) {
      respond(bad);
      expect(await client().closeInitiative("in-1", "Done")).toBe(FALLBACK_INITIATIVE);
    }
  });
});
