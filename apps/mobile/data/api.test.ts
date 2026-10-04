import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { api } from "./api";

// api.ts refuses to load without a base URL; set it before the import runs.
vi.hoisted(() => {
  process.env.EXPO_PUBLIC_API_URL = "https://api.example.test";
});

// The real store pulls in expo-secure-store; the client only needs the slug.
vi.mock("@/data/workspace-store", () => ({ getCurrentSlug: () => null }));

describe("api.deleteComment", () => {
  const fetchMock = vi.fn(async () => new Response(null, { status: 204 }));

  beforeEach(() => {
    fetchMock.mockClear();
    vi.stubGlobal("fetch", fetchMock);
    vi.spyOn(console, "log").mockImplementation(() => {});
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  // #8296: servers that keep a deleted comment's replies also route
  // /keep-replies; older servers do not, so a keep-replies delete that reaches
  // one fails instead of deleting the replies too.
  it.each([
    [{ keepReplies: true }, "https://api.example.test/api/comments/comment-1/keep-replies"],
    [{ keepReplies: false }, "https://api.example.test/api/comments/comment-1"],
    [undefined, "https://api.example.test/api/comments/comment-1"],
  ])("with %j sends DELETE %s", async (opts, url) => {
    await api.deleteComment("comment-1", opts);

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledWith(url, expect.objectContaining({ method: "DELETE" }));
  });
});

describe("api.listInboxPage", () => {
  const body = { items: [], next_cursor: null, has_more: false };
  const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) =>
    new Response(JSON.stringify(body), { status: 200 }),
  );

  beforeEach(() => {
    fetchMock.mockClear();
    vi.stubGlobal("fetch", fetchMock);
    vi.spyOn(console, "log").mockImplementation(() => {});
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it.each([
    [{}, "https://api.example.test/api/inbox/page?limit=50"],
    [{ cursor: "abc" }, "https://api.example.test/api/inbox/page?limit=50&cursor=abc"],
    [{ cursor: null }, "https://api.example.test/api/inbox/page?limit=50"],
    [{ groupId: "group-1" }, "https://api.example.test/api/inbox/page?limit=50&group_id=group-1"],
  ])("with %j requests %s", async (opts, url) => {
    await expect(api.listInboxPage(opts)).resolves.toEqual({
      items: [],
      nextCursor: null,
      hasMore: false,
    });
    expect(fetchMock).toHaveBeenCalledWith(url, expect.anything());
  });

  it("forwards the query's abort signal to the request", async () => {
    const controller = new AbortController();
    controller.abort();
    await api.listInboxPage({ signal: controller.signal });
    expect(fetchMock.mock.calls[0]?.[1]?.signal?.aborted).toBe(true);
  });

  it("throws on a malformed page instead of reading as an empty inbox", async () => {
    vi.spyOn(console, "warn").mockImplementation(() => {});
    fetchMock.mockResolvedValueOnce(
      new Response(JSON.stringify({ items: [], next_cursor: null, has_more: true }), {
        status: 200,
      }),
    );

    await expect(api.listInboxPage()).rejects.toThrow("Invalid inbox page response");
  });
});
