import { describe, expect, it, vi } from "vitest";
import { createAppClient } from "./index";

describe("createAppClient", () => {
  it("exchanges a personal app token and calls Registry v1 directly", async () => {
    const fetcher = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/api/cerebro/apps/app-id/token")) {
        return Response.json({ key: "sk_personal", session_id: "session-id", expires_at: "2099-01-01T00:00:00Z" }, { status: 201 });
      }
      expect(url).toBe("https://registry.example/api/registry/v1/data-sources/products/execute");
      expect(new Headers(init?.headers).get("authorization")).toBe("Bearer sk_personal");
      expect(JSON.parse(String(init?.body))).toMatchObject({ parameters: { country: "DK" }, pagination: { limit: 25 } });
      return Response.json({ rows: [{ sku: "A" }] });
    });
    const client = createAppClient({
      appId: "app-id",
      appVersion: "1.0.0",
      multicaBaseUrl: "https://multica.example",
      registryBaseUrl: "https://registry.example/api/registry/v1",
      fetch: fetcher,
    });

    await expect(client.data.source("products").execute({ parameters: { country: "DK" }, pagination: { limit: 25 } })).resolves.toEqual({ rows: [{ sku: "A" }] });
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it("renews a token inside the expiry skew", async () => {
    const fetcher = vi.fn(async () => Response.json({ key: "sk_short", session_id: "session-id", expires_at: new Date(Date.now() + 10_000).toISOString() }, { status: 201 }));
    const client = createAppClient({ appId: "app-id", appVersion: "1.0.0", multicaBaseUrl: "https://multica.example", registryBaseUrl: "https://registry.example/v1", fetch: fetcher });
    await client.auth.token();
    await client.auth.token();
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it("surfaces registry denials without retrying on a broader credential", async () => {
    const fetcher = vi.fn()
      .mockResolvedValueOnce(Response.json({ key: "sk_personal", session_id: "session-id", expires_at: "2099-01-01T00:00:00Z" }, { status: 201 }))
      .mockResolvedValueOnce(Response.json({ error: "App scope ceiling denied this resource" }, { status: 403 }));
    const client = createAppClient({ appId: "app-id", appVersion: "1.0.0", multicaBaseUrl: "https://multica.example", registryBaseUrl: "https://registry.example/v1", fetch: fetcher });
    await expect(client.data.source("secret").execute({})).rejects.toThrow(/403.*scope ceiling/i);
    expect(fetcher).toHaveBeenCalledTimes(2);
  });
});
