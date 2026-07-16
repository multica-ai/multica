import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const { cerebroRequest } = vi.hoisted(() => ({ cerebroRequest: vi.fn() }));
vi.mock("@multica/core/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/api")>();
  return { ...actual, api: { ...actual.api, cerebroRequest } };
});

import { callAppSdk, listApps } from "./api";

describe("mini apps API boundary", () => {
  beforeEach(() => cerebroRequest.mockReset());
  afterEach(() => vi.unstubAllGlobals());

  it("keeps the catalog rendering when an older server returns a malformed app", async () => {
    cerebroRequest.mockResolvedValue({ apps: [{ id: null, status: "future-status" }] });
    await expect(listApps()).resolves.toEqual({ apps: [] });
  });

  it("sends worker calls to the runtime origin instead of the Cerebro API", async () => {
    const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify({ formatted: "MILK" }), {
      status: 200,
      headers: { "content-type": "application/json" },
    }));
    vi.stubGlobal("fetch", fetcher);

    await expect(callAppSdk({
      appId: "app-1",
      version: "1.0.0",
      method: "workers.invoke",
      args: [{ ingredients: "milk" }],
    }, "https://apps.example/runtime/")).resolves.toEqual({ formatted: "MILK" });

    expect(fetcher).toHaveBeenCalledWith("https://apps.example/runtime/workers/app-1/1.0.0/invoke", expect.objectContaining({ method: "POST" }));
    expect(cerebroRequest).not.toHaveBeenCalled();
  });
});
