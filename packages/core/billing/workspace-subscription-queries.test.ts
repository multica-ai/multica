import { describe, expect, it } from "vitest";
import {
  workspaceSubscriptionPricesOptions,
  workspaceSubscriptionPricesStaleTime,
} from "./workspace-subscription-queries";

const validPrices = {
  month: {
    currency: "usd",
    unitAmount: 1_000,
    interval: "month",
    intervalCount: 1,
  },
  year: {
    currency: "usd",
    unitAmount: 9_600,
    interval: "year",
    intervalCount: 1,
  },
} as const;

describe("workspace subscription price query", () => {
  it("keeps missing price responses stale so the UI can recover", () => {
    expect(workspaceSubscriptionPricesStaleTime(undefined)).toBe(0);
    expect(workspaceSubscriptionPricesStaleTime(null)).toBe(0);
    expect(workspaceSubscriptionPricesStaleTime(validPrices)).toBe(
      10 * 60 * 1_000,
    );
  });

  it("wires the data-aware stale time into focus revalidation", () => {
    const options = workspaceSubscriptionPricesOptions("workspace-1");
    const staleTime = options.staleTime as (query: {
      state: { data: typeof validPrices | null };
    }) => number;

    expect(typeof staleTime).toBe("function");
    expect(staleTime({ state: { data: null } })).toBe(0);
    expect(staleTime({ state: { data: validPrices } })).toBe(10 * 60 * 1_000);
    expect(options.refetchOnWindowFocus).toBe(true);
    expect(options.retry).toBe(false);
  });
});
