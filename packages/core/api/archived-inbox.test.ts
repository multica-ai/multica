// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import { EMPTY_INBOX_FILTERS } from "../inbox/filter-store";

afterEach(() => vi.unstubAllGlobals());

function respond(body: unknown) {
  const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 200 }));
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

describe("archived inbox API", () => {
  it("sends cursor, lookup and every filter dimension and maps the page", async () => {
    const fetch = respond({ items: [], next_cursor: null, has_more: false });
    const api = new ApiClient("https://api.example.test");
    await expect(api.listArchivedInboxPage({
      statuses: ["in_review"], priorities: ["high"], actors: ["system"], unreadOnly: true,
    }, { cursor: "cursor-value", groupId: "group" })).resolves.toEqual({ items: [], nextCursor: null, hasMore: false });
    const url = new URL(fetch.mock.calls[0]![0]);
    expect(url.pathname).toBe("/api/inbox/archived/page");
    expect(Object.fromEntries(url.searchParams)).toEqual({
      statuses: "in_review", priorities: "high", actors: "system", unread_only: "true",
      limit: "50", cursor: "cursor-value", group_id: "group",
    });
  });

  it.each([
    {}, [], { items: [] }, { items: "wrong", next_cursor: null, has_more: false },
    { items: [], next_cursor: null, has_more: true },
    { items: [], next_cursor: "next", has_more: true },
    { items: [{ id: "broken" }], next_cursor: null, has_more: false },
  ])("rejects malformed pages instead of presenting an empty archive: %j", async (body) => {
    respond(body);
    await expect(new ApiClient("https://api.example.test").listArchivedInboxPage(EMPTY_INBOX_FILTERS))
      .rejects.toThrow("Invalid archived inbox page response");
  });

  it("maps complete facets and rejects incomplete or invalid counts", async () => {
    respond({ statuses: { in_review: 2 }, priorities: {}, actors: { system: 0 }, unread_count: 1 });
    const api = new ApiClient("https://api.example.test");
    await expect(api.getArchivedInboxFacets(EMPTY_INBOX_FILTERS)).resolves.toEqual({
      statuses: { in_review: 2 }, priorities: {}, actors: { system: 0 }, unreadCount: 1,
    });
    for (const body of [{}, { statuses: {}, priorities: {}, actors: {}, unread_count: -1 }]) {
      respond(body);
      await expect(api.getArchivedInboxFacets(EMPTY_INBOX_FILTERS)).rejects.toThrow("Invalid archived inbox facets response");
    }
  });
});
