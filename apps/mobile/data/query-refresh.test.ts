// @vitest-environment node
import { QueryClient, QueryObserver } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  createCoalescedRefresher,
  refreshActiveQueries,
} from "./query-refresh";

describe("refreshActiveQueries", () => {
  let queryClient: QueryClient;

  afterEach(() => queryClient?.clear());

  it("refetches an active query even when its staleTime is Infinity", async () => {
    queryClient = new QueryClient();
    const queryFn = vi
      .fn()
      .mockResolvedValueOnce("first")
      .mockResolvedValueOnce("second");
    const observer = new QueryObserver(queryClient, {
      queryKey: ["active"],
      queryFn,
      staleTime: Infinity,
    });
    const unsubscribe = observer.subscribe(() => {});

    try {
      await vi.waitFor(() => {
        expect(observer.getCurrentResult().data).toBe("first");
      });
      await refreshActiveQueries(queryClient);
      await vi.waitFor(() => {
        expect(queryFn).toHaveBeenCalledTimes(2);
        expect(observer.getCurrentResult().data).toBe("second");
      });
    } finally {
      unsubscribe();
    }
  });

  it("leaves cached queries without a mounted observer alone", async () => {
    queryClient = new QueryClient();
    queryClient.setQueryData(["inactive"], "cached");

    await refreshActiveQueries(queryClient);

    expect(queryClient.getQueryData(["inactive"])).toBe("cached");
    expect(queryClient.getQueryState(["inactive"])?.fetchStatus).toBe("idle");
  });
});

describe("createCoalescedRefresher", () => {
  it("drops a second trigger inside the coalesce window", () => {
    let now = 1_000;
    const refresh = vi.fn();
    const trigger = createCoalescedRefresher({
      refresh,
      now: () => now,
      minIntervalMs: 800,
    });

    expect(trigger()).toBe(true);
    now += 200;
    expect(trigger()).toBe(false);
    now += 700;
    expect(trigger()).toBe(true);
    expect(refresh).toHaveBeenCalledTimes(2);
  });
});
