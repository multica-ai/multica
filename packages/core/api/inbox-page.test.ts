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

describe("active inbox page API", () => {
  it("sends cursor, lookup and every filter dimension and maps the page", async () => {
    const fetch = respond({ items: [], next_cursor: null, has_more: false });
    const api = new ApiClient("https://api.example.test");
    await expect(api.listInboxPage({
      statuses: ["in_review", "done"], priorities: ["high"], actors: ["system"], unreadOnly: true,
    }, { cursor: "cursor-value", groupId: "group" })).resolves.toEqual({ items: [], nextCursor: null, hasMore: false });
    const url = new URL(fetch.mock.calls[0]![0]);
    expect(url.pathname).toBe("/api/inbox/page");
    expect(Object.fromEntries(url.searchParams)).toEqual({
      statuses: "done,in_review", priorities: "high", actors: "system", unread_only: "true",
      limit: "50", cursor: "cursor-value", group_id: "group",
    });
  });

  it("omits empty filters", async () => {
    const fetch = respond({ items: [], next_cursor: null, has_more: false });
    await new ApiClient("https://api.example.test").listInboxPage(EMPTY_INBOX_FILTERS);
    const url = new URL(fetch.mock.calls[0]![0]);
    expect(Object.fromEntries(url.searchParams)).toEqual({ limit: "50" });
  });

  it.each([
    {}, [], { items: [] }, { items: "wrong", next_cursor: null, has_more: false },
    { items: [], next_cursor: null, has_more: true },
    { items: [], next_cursor: "next", has_more: true },
    { items: [], next_cursor: "", has_more: false },
    { items: [{ id: "broken" }], next_cursor: null, has_more: false },
  ])("rejects malformed pages instead of presenting an empty inbox: %j", async (body) => {
    respond(body);
    await expect(new ApiClient("https://api.example.test").listInboxPage(EMPTY_INBOX_FILTERS))
      .rejects.toThrow("Invalid inbox page response");
  });

  it("maps complete facets and rejects incomplete or invalid counts", async () => {
    const fetch = respond({ statuses: { in_review: 2 }, priorities: {}, actors: { system: 0 }, unread_count: 1 });
    const api = new ApiClient("https://api.example.test");
    await expect(api.getInboxFacets({ ...EMPTY_INBOX_FILTERS, unreadOnly: true })).resolves.toEqual({
      statuses: { in_review: 2 }, priorities: {}, actors: { system: 0 }, unreadCount: 1,
    });
    const url = new URL(fetch.mock.calls[0]![0]);
    expect(url.pathname).toBe("/api/inbox/facets");
    expect(Object.fromEntries(url.searchParams)).toEqual({ unread_only: "true" });
    for (const body of [{}, [], { statuses: {}, priorities: {}, actors: {}, unread_count: -1 },
      { statuses: { done: 1.5 }, priorities: {}, actors: {}, unread_count: 0 }]) {
      respond(body);
      await expect(api.getInboxFacets(EMPTY_INBOX_FILTERS)).rejects.toThrow("Invalid inbox facets response");
    }
  });
});
