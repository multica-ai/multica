import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";

afterEach(() => vi.unstubAllGlobals());

describe("ApiClient runtime setup sessions", () => {
  it("creates and reads a schema-validated setup session", async () => {
    const created = {
      id: "session-1",
      token: "mst_secret",
      expires_at: "2026-07-21T12:30:00Z",
      redeemed_at: null,
      daemon_connected_at: null,
      daemon_id: null,
      runtime_count: 0,
    };
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(JSON.stringify(created), {
          status: 201,
          headers: { "Content-Type": "application/json" },
        }),
      )
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            ...created,
            token: undefined,
            redeemed_at: "2026-07-21T12:01:00Z",
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    await expect(client.createRuntimeSetupSession("workspace-1")).resolves.toEqual(created);
    await expect(
      client.getRuntimeSetupSession("workspace-1", "session-1"),
    ).resolves.toMatchObject({ id: "session-1", redeemed_at: "2026-07-21T12:01:00Z" });
    expect(fetchMock.mock.calls[0]?.[0]).toBe(
      "https://api.example.test/api/workspaces/workspace-1/setup-tokens",
    );
  });

  it("fails closed when the secret-bearing create response is malformed", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ id: "session-1", runtime_count: "zero" }), {
          status: 201,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );

    const client = new ApiClient("https://api.example.test");
    await expect(client.createRuntimeSetupSession("workspace-1")).rejects.toThrow(
      /missing its token/,
    );
  });
});
