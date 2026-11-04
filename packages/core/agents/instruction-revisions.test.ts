// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { EMPTY_INSTRUCTION_REVISIONS, parseInstructionRevisions } from "./instruction-revisions";

afterEach(() => {
  vi.unstubAllGlobals();
});

const client = () => new ApiClient("https://api.test");

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

function sent(fetchMock: ReturnType<typeof respond>) {
  const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
  return { method: init.method ?? "GET", path: url.replace("https://api.test", "") };
}

const REVISION = {
  id: "rev-1",
  agent_id: "ag-1",
  revision: 2,
  instructions: "Be concise.",
  source: "proposal",
  proposal_id: "p-1",
  actor_type: "member",
  actor_id: "u-1",
  created_at: "2026-11-02T10:00:00Z",
};

describe("instruction revision list", () => {
  it("reads the revisions in the order the server sends them", () => {
    const list = parseInstructionRevisions({ revisions: [REVISION, { ...REVISION, id: "rev-0", revision: 1, source: "human", proposal_id: null }] });
    expect(list.map((item) => item.revision)).toEqual([2, 1]);
    expect(list[0]).toMatchObject({ source: "proposal", proposal_id: "p-1", actor_id: "u-1" });
    expect(list[1]).toMatchObject({ source: "human", proposal_id: null });
  });

  it("degrades an unknown source to unknown rather than dropping the revision", () => {
    const [item] = parseInstructionRevisions({ revisions: [{ ...REVISION, source: "imported" }] });
    expect(item?.source).toBe("unknown");
  });

  it("reads a response with no revisions key as an empty history", () => {
    expect(parseInstructionRevisions({})).toEqual(EMPTY_INSTRUCTION_REVISIONS);
  });

  it("reads a malformed response as an empty history instead of throwing", () => {
    expect(parseInstructionRevisions({ revisions: "nope" })).toEqual(EMPTY_INSTRUCTION_REVISIONS);
    expect(parseInstructionRevisions(null)).toEqual(EMPTY_INSTRUCTION_REVISIONS);
  });

  it("GET lists an agent's revisions from its history endpoint", async () => {
    const fetchMock = respond({ revisions: [REVISION] });
    const list = await client().listAgentInstructionRevisions("ag-1");
    expect(sent(fetchMock)).toEqual({ method: "GET", path: "/api/agents/ag-1/instruction-revisions" });
    expect(list).toHaveLength(1);
    expect(list[0]?.instructions).toBe("Be concise.");
  });
});

describe("instruction revision restore", () => {
  it("POST restores the chosen revision", async () => {
    const fetchMock = respond({ agent: {}, revision: REVISION });
    await client().restoreAgentInstructionRevision("ag-1", 2);
    expect(sent(fetchMock)).toEqual({
      method: "POST",
      path: "/api/agents/ag-1/instruction-revisions/2/restore",
    });
  });

  it("surfaces a refusal from the server as an error", async () => {
    respond({ error: "the agent already has the instructions of this revision" }, 409);
    await expect(client().restoreAgentInstructionRevision("ag-1", 2)).rejects.toMatchObject({ status: 409 });
  });
});
