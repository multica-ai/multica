// @vitest-environment node
import { afterEach, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import { LarkSendObservationResponseSchema } from "./schemas";

const observation = { observed_at: "2026-11-02T00:00:00.000000001Z", result: "api_confirmed", reason: "none" };
afterEach(() => vi.unstubAllGlobals());

it("parses confirmed, absent and future observation states without retaining extra fields", () => {
  expect(LarkSendObservationResponseSchema.parse({ observation: null })).toEqual({ available: true, observation: null });
  expect(LarkSendObservationResponseSchema.parse({ observation }).observation?.result).toBe("api_confirmed");
  expect(LarkSendObservationResponseSchema.parse({ observation: { ...observation, result: "future", reason: "future", raw: "secret" } })).toEqual({ available: true, observation: { ...observation, result: "uncertain", reason: "unconfirmed" } });
});

it.each([{}, { observation: false }, { observation: { ...observation, observed_at: "yesterday" } }, { observation: { result: "api_confirmed" } }])("marks malformed responses unavailable: %j", async (data) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(data))));
  expect(await new ApiClient("https://example.test").getLarkSendObservation("ws", "inst")).toEqual({ available: false, observation: null });
});

it("reads only the workspace-scoped observation endpoint and preserves access errors", async () => {
  const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify({ observation }), { status: 200 }));
  vi.stubGlobal("fetch", fetcher);
  const client = new ApiClient("https://example.test");
  expect((await client.getLarkSendObservation("ws", "inst")).observation).toEqual(observation);
  expect(String(fetcher.mock.calls[0]?.[0])).toBe("https://example.test/api/workspaces/ws/lark/installations/inst/send-observation");
  fetcher.mockResolvedValue(new Response(JSON.stringify({ error: "forbidden" }), { status: 403 }));
  await expect(client.getLarkSendObservation("ws", "inst")).rejects.toMatchObject({ status: 403 });
});
