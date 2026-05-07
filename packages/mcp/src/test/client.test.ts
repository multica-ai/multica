import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, MulticaClient } from "../client.js";

interface CapturedRequest {
  url: string;
  init: RequestInit;
}

function mockFetch(handler: (req: CapturedRequest) => Response | Promise<Response>) {
  const captured: CapturedRequest[] = [];
  const fetchSpy = vi.fn(async (url: string, init: RequestInit) => {
    const req = { url, init };
    captured.push(req);
    return handler(req);
  });
  vi.stubGlobal("fetch", fetchSpy);
  return { captured };
}

const baseConfig = {
  apiUrl: "https://api.example.com",
  token: "mul_test",
  defaultWorkspaceId: "ws-1",
};

describe("MulticaClient", () => {
  beforeEach(() => {
    vi.useRealTimers();
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("sends Authorization + X-Workspace-ID by default and parses JSON responses", async () => {
    const { captured } = mockFetch(() => new Response(JSON.stringify({ ok: true }), { status: 200 }));
    const client = new MulticaClient(baseConfig);

    const result = await client.get<{ ok: boolean }>("/api/foo");

    expect(result).toEqual({ ok: true });
    expect(captured).toHaveLength(1);
    const headers = captured[0]!.init.headers as Record<string, string>;
    expect(headers["Authorization"]).toBe("Bearer mul_test");
    expect(headers["X-Workspace-ID"]).toBe("ws-1");
    expect(captured[0]!.url).toBe("https://api.example.com/api/foo");
  });

  it("merges query params into the URL", async () => {
    const { captured } = mockFetch(() => new Response("[]", { status: 200 }));
    const client = new MulticaClient(baseConfig);

    await client.get("/api/issues", { query: { limit: 10, status: "todo", skip: undefined } });

    const url = new URL(captured[0]!.url);
    expect(url.pathname).toBe("/api/issues");
    expect(url.searchParams.get("limit")).toBe("10");
    expect(url.searchParams.get("status")).toBe("todo");
    expect(url.searchParams.has("skip")).toBe(false);
  });

  it("includes JSON body and Content-Type on POST", async () => {
    const { captured } = mockFetch(() => new Response("{}", { status: 200 }));
    const client = new MulticaClient(baseConfig);

    await client.post("/api/issues", { title: "Hello" });

    expect(captured[0]!.init.method).toBe("POST");
    const headers = captured[0]!.init.headers as Record<string, string>;
    expect(headers["Content-Type"]).toBe("application/json");
    expect(captured[0]!.init.body).toBe(JSON.stringify({ title: "Hello" }));
  });

  it("returns null on 204 No Content", async () => {
    mockFetch(() => new Response(null, { status: 204 }));
    const client = new MulticaClient(baseConfig);
    await expect(client.delete("/api/issues/x")).resolves.toBeNull();
  });

  it("wraps non-2xx responses in ApiError with the body attached", async () => {
    mockFetch(
      () => new Response(JSON.stringify({ error: "not found" }), { status: 404 }),
    );
    const client = new MulticaClient(baseConfig);
    await expect(client.get("/api/issues/missing")).rejects.toMatchObject({
      name: "ApiError",
      status: 404,
      body: { error: "not found" },
    });
  });

  it("preserves raw text bodies that aren't JSON", async () => {
    mockFetch(() => new Response("plain string", { status: 500 }));
    const client = new MulticaClient(baseConfig);
    try {
      await client.get("/api/oops");
      throw new Error("expected ApiError");
    } catch (err) {
      expect(err).toBeInstanceOf(ApiError);
      const e = err as ApiError;
      expect(e.status).toBe(500);
      expect(e.body).toBe("plain string");
    }
  });

  it("respects per-call workspace override", async () => {
    const { captured } = mockFetch(() => new Response("{}", { status: 200 }));
    const client = new MulticaClient(baseConfig);

    await client.get("/api/foo", { workspaceId: "ws-2" });
    const headers = captured[0]!.init.headers as Record<string, string>;
    expect(headers["X-Workspace-ID"]).toBe("ws-2");
  });

  it("omits X-Workspace-ID when caller passes null and no default is set", async () => {
    const { captured } = mockFetch(() => new Response("{}", { status: 200 }));
    const client = new MulticaClient({ ...baseConfig, defaultWorkspaceId: null });

    await client.get("/api/foo", { workspaceId: null });
    const headers = captured[0]!.init.headers as Record<string, string>;
    expect(headers["X-Workspace-ID"]).toBeUndefined();
  });
});
