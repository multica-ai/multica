import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const { cerebroRequest } = vi.hoisted(() => ({ cerebroRequest: vi.fn() }));
vi.mock("@multica/core/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/api")>();
  return { ...actual, api: { ...actual.api, cerebroRequest } };
});

import { callAppSdk, listApps, publishAppVersion } from "./api";

describe("mini apps API boundary", () => {
  beforeEach(() => cerebroRequest.mockReset());
  afterEach(() => vi.unstubAllGlobals());

  it("keeps the catalog rendering when an older server returns a malformed app", async () => {
    cerebroRequest.mockResolvedValue({ apps: [{ id: null, status: "future-status" }] });
    await expect(listApps()).resolves.toEqual({ apps: [] });
  });

  it("sends worker calls through the member-bound Cerebro invoke endpoint", async () => {
    cerebroRequest.mockResolvedValue({ formatted: "MILK" });

    await expect(callAppSdk({
      appId: "app-1",
      version: "1.0.0",
      method: "workers.invoke",
      args: [{ ingredients: "milk" }],
    }, "https://apps.example/runtime/")).resolves.toEqual({ formatted: "MILK" });

    expect(cerebroRequest).toHaveBeenCalledWith("/api/cerebro/apps/app-1/invoke", {
      method: "POST",
      body: JSON.stringify({ ingredients: "milk" }),
    });
  });

  it("publishes immutable file bytes with a digest and workspace context", async () => {
    cerebroRequest.mockResolvedValue({ deployment_status: "provisioning" });

    await expect(publishAppVersion("app-1", {
      version: "1.2.3",
      release_notes: "Ready for staging",
      files: [{ path: "app.json", media_type: "application/json", content: "{}" }],
    }, "firtal")).resolves.toEqual({ deployment_status: "provisioning" });

    expect(cerebroRequest).toHaveBeenCalledWith("/api/cerebro/apps/app-1/publish?workspace_slug=firtal", {
      method: "POST",
      body: JSON.stringify({
        version: "1.2.3",
        release_notes: "Ready for staging",
        files: [{
          path: "app.json",
          media_type: "application/json",
          content_base64: "e30=",
          sha256: "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a",
        }],
      }),
    });
  });

  it("rejects a malformed publish response", async () => {
    cerebroRequest.mockResolvedValue({ status: "ready" });
    await expect(publishAppVersion("app-1", {
      version: "1.0.0",
      release_notes: "Initial",
      files: [],
    })).rejects.toThrow("The app publish response was invalid");
  });
});
