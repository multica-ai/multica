// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "./query-client";

describe("createQueryClient on the server", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  // A server render builds a fresh client per request. A pending cache GC
  // timer is created inside the request's async context, so it keeps the
  // whole render reachable until it fires (#8113).
  it("leaves no cache GC timer behind a query fetched during a render", async () => {
    vi.useFakeTimers();
    const client = createQueryClient();

    await client.prefetchQuery({ queryKey: ["ssr"], queryFn: () => "value" });
    await vi.advanceTimersByTimeAsync(0);

    expect(client.getQueryData(["ssr"])).toBe("value");
    expect(vi.getTimerCount()).toBe(0);
  });
});
