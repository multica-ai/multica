// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";

afterEach(() => vi.unstubAllGlobals());

function mockResponse(body: unknown) {
  const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(body)));
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

describe("cancel queued agent tasks", () => {
  const client = new ApiClient("https://api.example.test");

  it("sends the confirmed task IDs and returns both counts", async () => {
    const fetch = mockResponse({ cancelled: 1, skipped: 1 });
    expect(await client.cancelQueuedAgentTasks("agent-1", ["a", "b"])).toEqual({
      cancelled: 1,
      skipped: 1,
    });
    expect(fetch).toHaveBeenCalledWith(
      "https://api.example.test/api/agents/agent-1/cancel-queued-tasks",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ task_ids: ["a", "b"] }),
      }),
    );
  });

  it.each([{}, { cancelled: 1 }, { cancelled: -1, skipped: 0 }, { cancelled: 1, skipped: "0" }])(
    "does not report success for a malformed response %j",
    async (body) => {
      mockResponse(body);
      expect(await client.cancelQueuedAgentTasks("agent-1", ["a"])).toBeNull();
    },
  );
});
