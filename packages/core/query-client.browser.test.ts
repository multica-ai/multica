// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "./query-client";

describe("createQueryClient in the browser", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("drops an unused query after ten minutes", async () => {
    vi.useFakeTimers();
    const client = createQueryClient();

    await client.prefetchQuery({ queryKey: ["browser"], queryFn: () => "value" });
    await vi.advanceTimersByTimeAsync(10 * 60 * 1000 - 1);
    expect(client.getQueryData(["browser"])).toBe("value");

    await vi.advanceTimersByTimeAsync(1);
    expect(client.getQueryData(["browser"])).toBeUndefined();
  });
});
