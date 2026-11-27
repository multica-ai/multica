import { describe, expect, it } from "vitest";
import {
  workspaceSubscriptionPricesOptions,
  workspaceSubscriptionPricesStaleTime,
} from "./workspace-subscription-queries";

const planPrices = (monthAmount: number, yearAmount: number) =>
  ({
    month: {
      currency: "usd",
      unitAmount: monthAmount,
      interval: "month",
      intervalCount: 1,
    },
    year: {
      currency: "usd",
      unitAmount: yearAmount,
      interval: "year",
      intervalCount: 1,
    },
  }) as const;

const validPrices = {
  pro: planPrices(1_500, 14_400),
  business: planPrices(3_000, 28_800),
  autopilotAddon: null,
};

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
