// Cerebro fork-only tests for ApiClient. Upstream's @multica/core/api/client
// has been augmented with feature-flag endpoints; the tests live here so the
// upstream client.test.ts file stays in sync with multica-ai/multica.

import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "@multica/core/api/client";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("ApiClient (cerebro feature flags)", () => {
  it("listFeatureFlags GETs the workspace feature-flags endpoint", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ overrides: { cerebro_channels: false } }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    const res = await client.listFeatureFlags("ws-1");

    expect(res.overrides).toEqual({ cerebro_channels: false });
    const [url, init] = fetchMock.mock.calls[0]!;
    expect(url).toBe("https://api.example.test/api/workspaces/ws-1/feature-flags");
    expect(init?.method ?? "GET").toBe("GET");
  });

  it("setFeatureFlag PUTs to the per-flag endpoint with the enabled body", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(null, { status: 204 }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    await client.setFeatureFlag("ws-1", "cerebro_channels", false);

    const [url, init] = fetchMock.mock.calls[0]!;
    expect(url).toBe("https://api.example.test/api/workspaces/ws-1/feature-flags/cerebro_channels");
    expect(init?.method).toBe("PUT");
    expect(init?.body).toBe(JSON.stringify({ enabled: false }));
  });
});
