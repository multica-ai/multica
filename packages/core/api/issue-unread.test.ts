// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";

afterEach(() => vi.unstubAllGlobals());

const mark = (issueId: string) => ({
  issue_id: issueId,
  outcome: "completed",
  agent_id: "agent-1",
  created_at: "2026-10-31T10:00:00Z",
});

describe("listIssueUnread", () => {
  it("follows next_cursor until the last page", async () => {
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(Response.json({ items: [mark("i-1"), mark("i-2")], next_cursor: "c 1|x" }))
      .mockResolvedValueOnce(Response.json({ items: [mark("i-3")], next_cursor: null }));
    vi.stubGlobal("fetch", fetch);

    const items = await new ApiClient("https://api.example.test").listIssueUnread();

    expect(items.map((item) => item.issue_id)).toEqual(["i-1", "i-2", "i-3"]);
    expect(fetch.mock.calls.map(([url]) => new URL(url).searchParams.get("cursor"))).toEqual([null, "c 1|x"]);
  });

  it("stops when a page repeats the cursor it was asked for", async () => {
    const fetch = vi.fn().mockImplementation(async () => Response.json({ items: [], next_cursor: "same" }));
    vi.stubGlobal("fetch", fetch);

    await new ApiClient("https://api.example.test").listIssueUnread();

    expect(fetch).toHaveBeenCalledTimes(2);
  });
});
