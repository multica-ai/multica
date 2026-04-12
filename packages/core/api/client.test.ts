import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";

describe("ApiClient request IDs", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("falls back when crypto.randomUUID is unavailable", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ id: "user-1" }),
    });

    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("crypto", {});

    const client = new ApiClient("http://example.com");
    await client.getMe();

    expect(fetchMock).toHaveBeenCalledOnce();
    const init = fetchMock.mock.calls[0]?.[1] as RequestInit;
    const headers = init.headers as Record<string, string>;

    expect(headers["X-Request-ID"]).toMatch(/^[a-z0-9]{8}$/);
  });
});
