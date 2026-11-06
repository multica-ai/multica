// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "./client";
import { EmptyAgentBuilderSessionPageSchema } from "./schemas";

const row = {
  session_id: "01922aaa-0000-7000-8000-000000000001",
  runtime_id: "01922aaa-0000-7000-8000-000000000002",
  created_at: "2026-10-31T12:00:00Z",
  machine_label: "Offline host",
  snapshot: "a".repeat(64),
};
afterEach(() => vi.unstubAllGlobals());
describe("safe builder recovery boundary", () => {
  it("parses minimal metadata and retains the continuation", () => {
    expect(
      EmptyAgentBuilderSessionPageSchema.parse({
        sessions: [row],
        next_cursor: "page2",
      }),
    ).toEqual({
      sessions: [
        {
          sessionId: row.session_id,
          runtimeId: row.runtime_id,
          createdAt: row.created_at,
          machineLabel: row.machine_label,
          snapshot: row.snapshot,
        },
      ],
      nextCursor: "page2",
    });
  });
  it.each([
    {},
    { sessions: [], next_cursor: 42 },
    { sessions: [] },
    { sessions: [row, row], next_cursor: "" },
    ...["session_id", "runtime_id", "created_at", "snapshot"].map((key) => ({
      sessions: [{ ...row, [key]: "bad" }],
      next_cursor: "",
    })),
  ])(
    "rejects malformed data without declaring discovery complete: %j",
    (raw) => {
      expect(EmptyAgentBuilderSessionPageSchema.safeParse(raw).success).toBe(
        false,
      );
    },
  );
  it("rejects malformed success responses", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(new Response('{"sessions":[]}', { status: 200 })),
    );
    await expect(
      new ApiClient("https://api.test").listEmptyAgentBuilderSessions(
        "workspace",
        "profile",
      ),
    ).rejects.toThrow("Invalid empty builder");
  });
  it.each([403, 404, 409, 500])(
    "propagates %s and never falls back to ordinary DELETE",
    async (status) => {
      const fetch = vi
        .fn()
        .mockImplementation(() =>
          Promise.resolve(new Response('{"error":"unavailable"}', { status })),
        );
      vi.stubGlobal("fetch", fetch);
      const client = new ApiClient("https://api.test");
      await expect(
        client.listEmptyAgentBuilderSessions("workspace", "profile"),
      ).rejects.toBeInstanceOf(ApiError);
      await expect(
        client.discardEmptyAgentBuilderSession(
          "workspace",
          "profile",
          row.session_id,
          row.snapshot,
        ),
      ).rejects.toBeInstanceOf(ApiError);
      expect(fetch).toHaveBeenCalledTimes(2);
      expect(fetch.mock.calls.map(([, init]) => init.method ?? "GET")).toEqual([
        "GET",
        "POST",
      ]);
      expect(fetch.mock.calls[1]?.[0]).toContain("/discard-empty");
    },
  );
  it("scopes both operations to the requested workspace and profile", async () => {
    const fetch = vi
      .fn()
      .mockImplementation((_url, init) =>
        Promise.resolve(
          init.method === "POST"
            ? new Response(null, { status: 204 })
            : new Response(
                JSON.stringify({ sessions: [row], next_cursor: "next" }),
                { status: 200 },
              ),
        ),
      );
    vi.stubGlobal("fetch", fetch);
    const client = new ApiClient("https://api.test");
    await client.listEmptyAgentBuilderSessions("ws-2", "profile-2", "cursor+/");
    await client.discardEmptyAgentBuilderSession(
      "ws-2",
      "profile-2",
      row.session_id,
      row.snapshot,
    );
    expect(fetch.mock.calls[0]?.[0]).toContain(
      "profile_id=profile-2&cursor=cursor%2B%2F",
    );
    for (const [, init] of fetch.mock.calls)
      expect(init.headers).toMatchObject({
        "X-Workspace-ID": "ws-2",
        "X-Workspace-Slug": "",
      });
    expect(JSON.parse(fetch.mock.calls[1]?.[1].body)).toEqual({
      profile_id: "profile-2",
      snapshot: row.snapshot,
    });
  });
});
