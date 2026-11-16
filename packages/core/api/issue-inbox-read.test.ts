// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";

afterEach(() => vi.unstubAllGlobals());

const read = (body: unknown) => {
  const fetch = vi.fn().mockResolvedValue(Response.json(body));
  vi.stubGlobal("fetch", fetch);
  return { fetch, result: new ApiClient("https://api.example.test").markIssueInboxRead("issue-1") };
};

describe("markIssueInboxRead", () => {
  it("posts to the issue's inbox read route and returns the count", async () => {
    const { fetch, result } = read({ count: 2, extra: true });

    await expect(result).resolves.toEqual({ count: 2, extra: true });
    const [url, init] = fetch.mock.calls[0]!;
    expect(new URL(url).pathname).toBe("/api/inbox/issues/issue-1/read");
    expect(init?.method).toBe("POST");
  });

  it("returns a zero count as zero", async () => {
    await expect(read({ count: 0 }).result).resolves.toEqual({ count: 0 });
  });

  // The write succeeded but the body cannot say whether rows changed. `null`
  // makes the caller refresh rather than read it as "nothing changed".
  it.each([
    ["a missing count", {}],
    ["a string count", { count: "2" }],
    ["a negative count", { count: -1 }],
    ["a fractional count", { count: 1.5 }],
    ["a null body", null],
  ])("reports an unknown count for %s", async (_name, body) => {
    await expect(read(body).result).resolves.toEqual({ count: null });
  });
});
