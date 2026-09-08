import { QueryClient } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import {
  billingBalanceOptions,
  billingBatchesOptions,
  billingCheckoutSessionOptions,
  billingKeys,
  billingPriceTiersOptions,
  billingTopupsOptions,
  billingTransactionsOptions,
} from "./queries";

describe("billing query options", () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
  });

  afterEach(() => {
    queryClient.clear();
    vi.restoreAllMocks();
  });

  it("uses account-level keys and keeps pagination parameters in list keys", () => {
    expect(billingKeys.balance()).toEqual(["billing", "balance"]);
    expect(billingKeys.transactions()).toEqual([
      "billing",
      "transactions",
      {},
    ]);
    expect(billingKeys.transactions({ page: 2, page_size: 25 })).toEqual([
      "billing",
      "transactions",
      { page: 2, page_size: 25 },
    ]);
    expect(billingKeys.batches({ page: 3 })).toEqual([
      "billing",
      "batches",
      { page: 3 },
    ]);
    expect(billingKeys.topups({ page_size: 50 })).toEqual([
      "billing",
      "topups",
      { page_size: 50 },
    ]);
  });

  it("forwards pagination parameters to every paginated billing endpoint", async () => {
    const params = { page: 2, page_size: 25 };
    const listCloudBillingTransactions = vi.fn().mockResolvedValue({
      items: [],
      total: 0,
      ...params,
    });
    const listCloudBillingBatches = vi.fn().mockResolvedValue({
      items: [],
      total: 0,
      ...params,
    });
    const listCloudBillingTopups = vi.fn().mockResolvedValue({
      items: [],
      total: 0,
      ...params,
    });
    setApiInstance({
      listCloudBillingTransactions,
      listCloudBillingBatches,
      listCloudBillingTopups,
    } as unknown as ApiClient);

    await Promise.all([
      queryClient.fetchQuery(billingTransactionsOptions(params)),
      queryClient.fetchQuery(billingBatchesOptions(params)),
      queryClient.fetchQuery(billingTopupsOptions(params)),
    ]);

    expect(listCloudBillingTransactions).toHaveBeenCalledWith(params);
    expect(listCloudBillingBatches).toHaveBeenCalledWith(params);
    expect(listCloudBillingTopups).toHaveBeenCalledWith(params);
  });

  it("uses short-lived caches for mutable data and a longer cache for price tiers", () => {
    expect(billingBalanceOptions().staleTime).toBe(30_000);
    expect(billingTransactionsOptions().staleTime).toBe(30_000);
    expect(billingBatchesOptions().staleTime).toBe(30_000);
    expect(billingTopupsOptions().staleTime).toBe(30_000);
    expect(billingPriceTiersOptions().staleTime).toBe(300_000);
  });
});

describe("billingCheckoutSessionOptions", () => {
  function refetchIntervalFor(status?: string) {
    const refetchInterval =
      billingCheckoutSessionOptions("cs_test").refetchInterval;
    expect(refetchInterval).toBeTypeOf("function");

    return (
      refetchInterval as (query: {
        state: { data?: { status: string } };
      }) => number | false
    )({
      state: { data: status === undefined ? undefined : { status } },
    });
  }

  it("keys and fetches a checkout session by its session id", async () => {
    const getCloudBillingCheckoutSession = vi.fn().mockResolvedValue({
      order_id: "order-1",
      status: "pending",
      amount_cents: 1_000,
      credits: 10_000,
      bonus_credits: 0,
      currency: "usd",
      tier_id: "starter",
    });
    setApiInstance({
      getCloudBillingCheckoutSession,
    } as unknown as ApiClient);
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const options = billingCheckoutSessionOptions("cs_test");

    await queryClient.fetchQuery(options);

    expect(options.queryKey).toEqual([
      "billing",
      "checkout-session",
      "cs_test",
    ]);
    expect(getCloudBillingCheckoutSession).toHaveBeenCalledWith("cs_test");
    queryClient.clear();
  });

  it.each(["credited", "failed", "canceled"])(
    "stops polling for terminal status %s",
    (status) => {
      expect(refetchIntervalFor(status)).toBe(false);
    },
  );

  it.each([undefined, "pending", "paid", "future_non_terminal_status"])(
    "continues polling for non-terminal status %s",
    (status) => {
      expect(refetchIntervalFor(status)).toBe(2_000);
    },
  );
});
