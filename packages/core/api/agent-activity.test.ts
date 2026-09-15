// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";

const bucket = {
  agent_id: "a",
  bucket_at: "2026-09-15T00:00:00Z",
  task_count: 10,
  failed_count: 1,
};

afterEach(() => vi.unstubAllGlobals());

async function read(body: unknown) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 200 })),
  );
  return new ApiClient("https://api.example.test").getWorkspaceAgentActivity30d();
}

describe("agent activity API outcomes", () => {
  it("preserves explicit outcome counts, including zero", async () => {
    expect(await read([{ ...bucket, completed_count: 0, cancelled_count: 9 }])).toEqual([
      { ...bucket, completed_count: 0, cancelled_count: 9 },
    ]);
  });

  // Outcome counts are part of the contract, so a bucket without usable
  // ones is drift, not an older peer to accommodate. Degrading the whole
  // response is the intended failure mode: an empty activity panel is
  // honest, a half-populated one silently misreports success.
  it.each([undefined, null, "1", -1, 1.5])(
    "drops the response when an outcome count is %s",
    async (bad) => {
      expect(
        await read([{ ...bucket, completed_count: bad, cancelled_count: bad }]),
      ).toEqual([]);
    },
  );

  it("falls back for a malformed list", async () => {
    expect(await read({ buckets: [] })).toEqual([]);
  });
});
