// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";

afterEach(() => vi.unstubAllGlobals());

describe("deleteInbox", () => {
  it("deletes the selected entry and parses the confirmed count", async () => {
    const fetch = vi.fn().mockResolvedValue(Response.json({ count: 2 }));
    vi.stubGlobal("fetch", fetch);
    await expect(new ApiClient("https://api.example.test").deleteInbox("item-1")).resolves.toEqual({ count: 2 });
    const [url, init] = fetch.mock.calls[0]!;
    expect(new URL(url).pathname).toBe("/api/inbox/item-1");
    expect(init.method).toBe("DELETE");
  });

  it.each([null, {}, { count: "2" }, { count: -1 }, { count: 0.5 }])("treats a malformed response as an unknown count: %j", async (body) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(body)));
    await expect(new ApiClient("https://api.example.test").deleteInbox("item-1")).resolves.toEqual({ count: null });
  });

  it("rejects a failed deletion", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ error: "not found" }, { status: 404 })));
    await expect(new ApiClient("https://api.example.test").deleteInbox("item-1")).rejects.toThrow();
  });
});
