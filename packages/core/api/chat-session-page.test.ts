// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";

const client = new ApiClient("http://localhost:8080");
afterEach(() => vi.unstubAllGlobals());

function respond(body: unknown) {
  const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } }));
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

describe("chat session page API boundary", () => {
  it("encodes the cursor and drops malformed rows", async () => {
    const fetch = respond({ sessions: [{ id: "one", title: "Demo" }, null, {}], has_more: true, next_cursor: "next+/=" });
    const page = await client.listChatSessionsPage({ status: "archived", cursor: "first+/=" });
    expect(page.sessions.map((s) => s.id)).toEqual(["one"]);
    expect(page.next_cursor).toBe("next+/=");
    const url = new URL(fetch.mock.calls[0]![0] as string);
    expect(url.searchParams.get("cursor")).toBe("first+/=");
    expect(url.searchParams.get("status")).toBe("archived");
    expect(url.searchParams.get("limit")).toBe("50");
  });

  it("keeps malformed pagination metadata from triggering another page", async () => {
    respond({ sessions: [], has_more: "yes", next_cursor: 42 });
    expect(await client.listChatSessionsPage({ status: "active" })).toEqual({ sessions: [], has_more: false, next_cursor: null });
  });

  it("defaults malformed summaries without crashing badge consumers", async () => {
    respond({ active_count: -1, archived_count: null, unread_by_session: { bad: "many" } });
    expect(await client.getChatSessionSummary()).toEqual({ active_count: 0, archived_count: 0, unread_by_session: {} });
  });
});
