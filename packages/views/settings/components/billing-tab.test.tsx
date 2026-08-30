import { act, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@multica/core/api";
import { configStore } from "@multica/core/config";
import { BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG } from "@multica/core/feature-flags";
import type {
  AutopilotQuotaUsage,
  IssueLimitUsage,
  WorkspaceSubscriptionEntitlements,
  WorkspaceSubscriptionSummary,
} from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const mocks = vi.hoisted(() => ({
  useQuery: vi.fn(),
  checkout: vi.fn(),
  portal: vi.fn(),
  previewSeats: vi.fn(),
  purchaseSeats: vi.fn(),
  refetch: vi.fn(),
  refetchSummary: vi.fn(),
  refetchUsage: vi.fn(),
  refetchIssueLimitUsage: vi.fn(),
  refetchPrices: vi.fn(),
  openExternal: vi.fn(),
  workspaceId: "workspace-1",
  prices: null as {
    month: {
      currency: string;
      unitAmount: number;
      interval: "month";
      intervalCount: number;
    };
    year: {
      currency: string;
      unitAmount: number;
      interval: "year";
      intervalCount: number;
    };
  } | null,
  pricesLoading: false,
  pricesFetching: false,
  pricesError: false,
  summaryPending: false,
  summaryFetching: false,
  summaryError: false,
  summaryMalformed: false,
  purchasePending: false,
  usagePending: false,
  usageFetching: false,
  usageError: false,
  summaryDataUpdatedAt: 0,
  summaryFetchedAfterMount: false,
  entitlements: {
    workspaceId: "workspace-1",
    plan: "free",
    status: "inactive",
    seats: 3,
    limits: {
      issueCount: { mode: "limited", limit: 17 },
      autopilotRuns: { mode: "limited", limit: 7 },
    },
    currentPeriodEnd: null as string | null,
    snapshotExpiresAt: null as string | null,
    version: 0,
  } as WorkspaceSubscriptionEntitlements,
  summary: {
    entitlement: null as unknown as WorkspaceSubscriptionEntitlements,
    billingInterval: null,
    humanMembers: 3,
    seatCapacity: null as {
      purchased: number;
      used: number;
      reserved: number;
      available: number;
      version: number;
      pendingQuantity: number | null;
      activePurchase: {
        requestId: string;
        targetSeats: number;
        status: "pending" | "processing" | "submitted";
        expiresAt: string | null;
      } | null;
    } | null,
    cancelAtPeriodEnd: false,
    graceUntil: null as string | null,
    hasStripeCustomer: false,
    availableActions: {
      checkout: true,
      portal: false,
      purchaseSeats: false,
    },
  } as WorkspaceSubscriptionSummary,
  issueLimitUsage: {
    used: 11,
    limit: 17,
  } as IssueLimitUsage,
  usage: {
    action: "enforce" as "off" | "observe" | "enforce",
    used: 3 as number | null,
    reserved: 2 as number | null,
    total: 5 as number | null,
    limit: 7 as number | null,
    reached: false as boolean | null,
    period_start: "2030-01-01T00:00:00Z" as string | null,
    period_end: "2030-02-01T00:00:00Z" as string | null,
    reset_at: "2030-02-01T00:00:00Z" as string | null,
    blocked_counts: {} as Record<string, number> | null,
  } as AutopilotQuotaUsage,
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: unknown) => mocks.useQuery(options),
}));

vi.mock("@multica/core/billing", () => ({
  workspaceSubscriptionPricesOptions: (wsId: string) => ({
    queryKey: ["workspace-subscriptions", wsId, "prices"],
  }),
  workspaceSubscriptionSummaryOptions: (wsId: string) => ({
    queryKey: ["workspace-subscriptions", wsId, "summary"],
  }),
  issueLimitUsageOptions: (wsId: string) => ({
    queryKey: ["workspace-subscriptions", wsId, "issue-limit-usage"],
  }),
  useCreateWorkspaceSubscriptionCheckout: () => ({
    mutateAsync: mocks.checkout,
    isPending: false,
  }),
  useCreateWorkspaceSubscriptionPortal: () => ({
    mutateAsync: mocks.portal,
    isPending: false,
  }),
  usePreviewWorkspaceSeatPurchase: () => ({
    mutateAsync: mocks.previewSeats,
    isPending: false,
  }),
  usePurchaseWorkspaceSeats: () => ({
    mutateAsync: mocks.purchaseSeats,
    isPending: mocks.purchasePending,
  }),
}));

vi.mock("@multica/core/autopilots", () => ({
  autopilotQuotaUsageOptions: (wsId: string) => ({
    queryKey: ["autopilots", wsId, "usage"],
  }),
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({
    id: mocks.workspaceId,
    slug: "acme",
    name: "Acme",
  }),
}));

const navigationState = {
  search: "tab=billing",
  replace: vi.fn(),
};
vi.mock("../../navigation", () => ({
  useNavigation: () => ({
    pathname: "/acme/settings",
    searchParams: new URLSearchParams(navigationState.search),
    hash: "",
    push: vi.fn(),
    replace: navigationState.replace,
    back: vi.fn(),
    getShareableUrl: vi.fn(),
  }),
}));

vi.mock("../../platform", () => ({ openExternal: mocks.openExternal }));

import { BillingTab, formatStripeMinorAmount } from "./billing-tab";

function setSeatCapacity({
  humanMembers = 4,
  purchased = 5,
  used = humanMembers,
  reserved = 0,
  available = Math.max(0, purchased - used - reserved),
  version = 9,
  pendingQuantity = null,
  activePurchase = null,
}: {
  humanMembers?: number;
  purchased?: number;
  used?: number;
  reserved?: number;
  available?: number;
  version?: number;
  pendingQuantity?: number | null;
  activePurchase?: NonNullable<typeof mocks.summary.seatCapacity>["activePurchase"];
} = {}) {
  mocks.summary.humanMembers = humanMembers;
  mocks.summary.seatCapacity = {
    purchased,
    used,
    reserved,
    available,
    overcommitted: used > purchased,
    version,
    pendingQuantity,
    activePurchase,
  };
  Object.assign(mocks.summary.availableActions, {
    checkout: false,
    portal: true,
    purchaseSeats: activePurchase === null,
  });
}

describe("BillingTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    navigationState.search = "tab=billing";
    mocks.workspaceId = "workspace-1";
    mocks.prices = {
      month: {
        currency: "usd",
        unitAmount: 1000,
        interval: "month",
        intervalCount: 1,
      },
      year: {
        currency: "usd",
        unitAmount: 9600,
        interval: "year",
        intervalCount: 1,
      },
    };
    mocks.pricesLoading = false;
    mocks.pricesFetching = false;
    mocks.pricesError = false;
    mocks.summaryPending = false;
    mocks.summaryFetching = false;
    mocks.summaryError = false;
    mocks.summaryMalformed = false;
    mocks.purchasePending = false;
    mocks.usagePending = false;
    mocks.usageFetching = false;
    mocks.usageError = false;
    mocks.summaryDataUpdatedAt = 0;
    mocks.summaryFetchedAfterMount = false;
    Object.assign(mocks.entitlements, {
      plan: "free",
      status: "inactive",
      seats: 3,
      limits: {
        issueCount: { mode: "limited", limit: 17 },
        autopilotRuns: { mode: "limited", limit: 7 },
      },
      currentPeriodEnd: null,
      snapshotExpiresAt: null,
      version: 0,
    });
    Object.assign(mocks.summary, {
      entitlement: mocks.entitlements,
      billingInterval: null,
      humanMembers: 3,
      seatCapacity: null,
      cancelAtPeriodEnd: false,
      graceUntil: null,
      hasStripeCustomer: false,
      availableActions: {
        checkout: true,
        portal: false,
        purchaseSeats: false,
      },
    });
    Object.assign(mocks.issueLimitUsage, {
      used: 11,
      limit: 17,
    });
    Object.assign(mocks.usage, {
      action: "enforce",
      used: 3,
      reserved: 2,
      total: 5,
      limit: 7,
      reached: false,
      period_start: "2030-01-01T00:00:00Z",
      period_end: "2030-02-01T00:00:00Z",
      reset_at: "2030-02-01T00:00:00Z",
      blocked_counts: {},
    });
    mocks.useQuery.mockImplementation((options: unknown) => {
      const queryKey = (options as { queryKey?: readonly unknown[] }).queryKey;
      if (queryKey?.[queryKey.length - 1] === "prices") {
        return {
          data: mocks.prices,
          isLoading: mocks.pricesLoading,
          isFetching: mocks.pricesFetching,
          isError: mocks.pricesError,
          refetch: mocks.refetchPrices,
        };
      }
      if (queryKey?.[queryKey.length - 1] === "summary") {
        return {
          data: mocks.summaryError
            ? undefined
            : mocks.summaryMalformed
              ? null
              : mocks.summary,
          isPending: mocks.summaryPending,
          isFetching: mocks.summaryFetching,
          isError: mocks.summaryError,
          dataUpdatedAt: mocks.summaryDataUpdatedAt,
          isFetchedAfterMount: mocks.summaryFetchedAfterMount,
          refetch: mocks.refetchSummary,
        };
      }
      if (queryKey?.[queryKey.length - 1] === "issue-limit-usage") {
        return {
          data: mocks.issueLimitUsage,
          isPending: false,
          isFetching: false,
          isError: false,
          refetch: mocks.refetchIssueLimitUsage,
        };
      }
      if (queryKey?.[queryKey.length - 1] === "usage") {
        return {
          data: mocks.usageError ? undefined : mocks.usage,
          isPending: mocks.usagePending,
          isFetching: mocks.usageFetching,
          isError: mocks.usageError,
        refetch: mocks.refetchUsage,
      };
    }
      return {
        data: mocks.entitlements,
        isPending: false,
        isError: false,
        refetch: mocks.refetch,
      };
    });
    mocks.checkout.mockResolvedValue({
      requestId: "request-1",
      sessionId: "cs_test_1",
      url: "https://checkout.stripe.com/test-session",
    });
    mocks.portal.mockResolvedValue({
      url: "https://billing.stripe.com/test-session",
    });
    mocks.refetchSummary.mockResolvedValue({ data: mocks.summary });
    mocks.previewSeats.mockResolvedValue({
      currentSeats: 5,
      additionalSeats: 1,
      resultingSeats: 6,
      purchaseVersion: 9,
      currency: "usd",
      prorationAmount: 250,
      nextInvoiceAmount: 6000,
      quotedAt: "2030-01-01T00:00:00Z",
    });
    mocks.purchaseSeats.mockResolvedValue({
      requestId: "seat-request-1",
      currentSeats: 5,
      additionalSeats: 1,
      resultingSeats: 6,
      currency: "usd",
      prorationAmount: 250,
      nextInvoiceAmount: 6000,
      status: "submitted",
    });
    configStore.getState().setFeatureFlags({
      [BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG]: true,
    });
  });

  it("renders nothing and issues no query when the feature flag is absent", () => {
    configStore.getState().setFeatureFlags({});

    const { container } = renderWithI18n(<BillingTab />);

    expect(container).toBeEmptyDOMElement();
    expect(mocks.useQuery).not.toHaveBeenCalled();
  });

  it("shows server prices and keeps the selected interval in sync", async () => {
    const user = userEvent.setup();
    renderWithI18n(<BillingTab />);

    expect(screen.getByRole("heading", { name: "Billing" })).toBeInTheDocument();
    expect(screen.getByText("11 / 17")).toBeInTheDocument();
    expect(screen.getByText("5 / 7")).toBeInTheDocument();
    expect(screen.getByText("3 completed · 2 in progress")).toBeInTheDocument();
    expect(screen.getByText("$10.00 per human seat")).toBeInTheDocument();
    expect(mocks.useQuery).toHaveBeenCalledWith(
      expect.objectContaining({
        queryKey: ["workspace-subscriptions", "workspace-1", "prices"],
      }),
    );

    await user.click(screen.getByRole("button", { name: "Yearly" }));

    expect(screen.getByText("$96.00 per human seat")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Retry" }),
    ).not.toBeInTheDocument();
  });

  it("formats Stripe minor units for decimal, zero-decimal, and special currencies", () => {
    expect(formatStripeMinorAmount(1234, "usd", "en-US")).toBe("$12.34");
    expect(formatStripeMinorAmount(1234, "jpy", "en-US")).toBe("¥1,234");
    expect(
      formatStripeMinorAmount(1234, "bhd", "en-US")?.replace(/\s/g, " "),
    ).toBe("BHD 1.234");
    expect(
      formatStripeMinorAmount(500, "ugx", "en-US")?.replace(/\s/g, " "),
    ).toBe("UGX 5");
    expect(
      formatStripeMinorAmount(1234, "huf", "en-US")?.replace(/\s/g, " "),
    ).toBe("HUF 12.34");
  });

  it("shows a local price skeleton without blocking Checkout while prices load", () => {
    mocks.prices = null;
    mocks.pricesLoading = true;

    renderWithI18n(<BillingTab />);

    expect(
      screen.getByRole("status", { name: "Loading subscription prices" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Upgrade to Pro" }),
    ).toBeEnabled();
  });

  it("announces a price-only retry without blocking Checkout", () => {
    mocks.prices = null;
    mocks.pricesFetching = true;

    renderWithI18n(<BillingTab />);

    expect(screen.getByRole("button", { name: "Retry" })).toBeDisabled();
    expect(
      screen.getByRole("status", { name: "Loading subscription prices" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Upgrade to Pro" }),
    ).toBeEnabled();
  });

  it.each([
    ["query error", true],
    ["malformed response", false],
  ])(
    "falls back safely after a prices %s without blocking Checkout",
    async (_case, isError) => {
      const user = userEvent.setup();
      mocks.prices = null;
      mocks.pricesError = isError;

      renderWithI18n(<BillingTab />);

      expect(
        screen.getByText(
          /Stripe Checkout shows the authoritative per-seat price/,
        ),
      ).toBeInTheDocument();
      expect(screen.queryByText(/\$0/)).not.toBeInTheDocument();

      await user.click(screen.getByRole("button", { name: "Retry" }));
      expect(mocks.refetchPrices).toHaveBeenCalledOnce();

      await user.click(screen.getByRole("button", { name: "Upgrade to Pro" }));
      await user.click(
        screen.getByRole("button", { name: "Continue to Stripe" }),
      );

      await waitFor(() => expect(mocks.checkout).toHaveBeenCalledOnce());
    },
  );

  it("uses workspace-scoped price query keys after a workspace switch", () => {
    const { rerender } = renderWithI18n(<BillingTab />);

    mocks.workspaceId = "workspace-2";
    rerender(<BillingTab />);

    expect(mocks.useQuery).toHaveBeenCalledWith(
      expect.objectContaining({
        queryKey: ["workspace-subscriptions", "workspace-2", "prices"],
      }),
    );
    expect(mocks.useQuery).toHaveBeenCalledWith(
      expect.objectContaining({
        queryKey: ["workspace-subscriptions", "workspace-2", "summary"],
      }),
    );
    expect(mocks.useQuery).toHaveBeenCalledWith(
      expect.objectContaining({
        queryKey: ["autopilots", "workspace-2", "usage"],
      }),
    );
  });

  it("shows the unit price without a zero estimated total when seats are unavailable", () => {
    mocks.entitlements.seats = 0;
    mocks.summary.humanMembers = 0;

    renderWithI18n(<BillingTab />);

    expect(screen.getByText("$10.00 per human seat")).toBeInTheDocument();
    expect(screen.queryByText(/Estimated monthly total/)).not.toBeInTheDocument();
    expect(screen.queryByText(/\$0/)).not.toBeInTheDocument();
  });

  it("creates Checkout with a client idempotency key and opens Stripe externally", async () => {
    const user = userEvent.setup();
    renderWithI18n(<BillingTab />);

    await user.click(screen.getByRole("button", { name: "Upgrade to Pro" }));
    await user.click(screen.getByRole("button", { name: "Continue to Stripe" }));

    await waitFor(() => {
      expect(mocks.checkout).toHaveBeenCalledTimes(1);
    });
    const request = mocks.checkout.mock.calls[0]?.[0] as {
      interval: string;
      idempotencyKey: string;
    };
    expect(request.interval).toBe("month");
    expect(request.idempotencyKey).toMatch(/^workspace-checkout-workspace-1-/);
    expect(Object.keys(request).sort()).toEqual(["idempotencyKey", "interval"]);
    expect(mocks.openExternal).toHaveBeenCalledWith(
      "https://checkout.stripe.com/test-session",
      { webTarget: "same-tab" },
    );
  });

  it("reuses the Checkout idempotency key after an ambiguous failure", async () => {
    const user = userEvent.setup();
    mocks.checkout
      .mockRejectedValueOnce(new Error("network lost after submit"))
      .mockResolvedValueOnce({
        requestId: "request-1",
        sessionId: "cs_test_1",
        url: "https://checkout.stripe.com/test-session",
      });
    renderWithI18n(<BillingTab />);

    await user.click(screen.getByRole("button", { name: "Upgrade to Pro" }));
    await user.click(screen.getByRole("button", { name: "Continue to Stripe" }));
    await screen.findByText("Billing action failed");

    await user.click(screen.getByRole("button", { name: "Upgrade to Pro" }));
    await user.click(screen.getByRole("button", { name: "Continue to Stripe" }));

    await waitFor(() => expect(mocks.checkout).toHaveBeenCalledTimes(2));
    expect(mocks.checkout.mock.calls[1]?.[0].idempotencyKey).toBe(
      mocks.checkout.mock.calls[0]?.[0].idempotencyKey,
    );
  });

  it("starts a new Checkout intent after explicit cancellation", async () => {
    const user = userEvent.setup();
    mocks.checkout
      .mockRejectedValueOnce(new Error("network lost after submit"))
      .mockResolvedValueOnce({
        requestId: "request-2",
        sessionId: "cs_test_2",
        url: "https://checkout.stripe.com/test-session-2",
      });
    renderWithI18n(<BillingTab />);

    await user.click(screen.getByRole("button", { name: "Upgrade to Pro" }));
    await user.click(screen.getByRole("button", { name: "Continue to Stripe" }));
    await screen.findByText("Billing action failed");

    await user.click(screen.getByRole("button", { name: "Upgrade to Pro" }));
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    await user.click(screen.getByRole("button", { name: "Upgrade to Pro" }));
    await user.click(screen.getByRole("button", { name: "Continue to Stripe" }));

    await waitFor(() => expect(mocks.checkout).toHaveBeenCalledTimes(2));
    expect(mocks.checkout.mock.calls[1]?.[0].idempotencyKey).not.toBe(
      mocks.checkout.mock.calls[0]?.[0].idempotencyKey,
    );
  });

  it.each([
    [503, "Billing is temporarily unavailable. Retry in a moment."],
    [
      403,
      "Your workspace role no longer allows this action. Refresh the page to see your current access.",
    ],
  ])("maps a %s Checkout response to actionable copy", async (status, copy) => {
    const user = userEvent.setup();
    mocks.checkout.mockRejectedValue(
      new ApiError("request failed", status, "Request Failed"),
    );
    renderWithI18n(<BillingTab />);

    await user.click(screen.getByRole("button", { name: "Upgrade to Pro" }));
    await user.click(screen.getByRole("button", { name: "Continue to Stripe" }));

    expect(await screen.findByText(copy)).toBeInTheDocument();
  });

  it("consumes cancel callback params once while preserving the active tab", () => {
    navigationState.search =
      "tab=billing&result=cancel&session_id=cs_test_1&source=email";

    renderWithI18n(<BillingTab />);

    expect(screen.getByText("Checkout canceled")).toBeInTheDocument();
    expect(navigationState.replace).toHaveBeenCalledOnce();
    expect(navigationState.replace).toHaveBeenCalledWith(
      "/acme/settings?tab=billing&source=email",
    );
  });

  it("polls after a success callback and surfaces a bounded sync timeout", () => {
    vi.useFakeTimers();
    navigationState.search = "tab=billing&result=success&session_id=cs_test_1";
    try {
      renderWithI18n(<BillingTab />);

      expect(
        screen.getByText("Activating your subscription"),
      ).toBeInTheDocument();
      const summaryOptions = mocks.useQuery.mock.calls
        .map(([options]) => options)
        .find(
          (options) =>
            options.queryKey?.[options.queryKey.length - 1] === "summary",
        );
      expect(
        summaryOptions.refetchInterval({ state: { data: mocks.summary } }),
      ).toBe(2_000);
      expect(navigationState.replace).toHaveBeenCalledWith(
        "/acme/settings?tab=billing",
      );

      act(() => vi.advanceTimersByTime(30_000));

      expect(
        screen.getByText(
          "Payment was received, but the subscription is still syncing. Refresh this page in a moment.",
        ),
      ).toBeInTheDocument();
      expect(navigationState.replace).toHaveBeenCalledOnce();
    } finally {
      vi.useRealTimers();
    }
  });

  it("keeps Checkout syncing while Cloud still exposes Checkout", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2030-01-01T00:00:00Z"));
    navigationState.search = "tab=billing&result=success&session_id=cs_test_1";
    Object.assign(mocks.entitlements, {
      plan: "pro",
      status: "active",
    });
    mocks.summaryDataUpdatedAt = Date.now() - 1;
    mocks.summaryFetchedAfterMount = true;
    try {
      renderWithI18n(<BillingTab />);

      expect(
        screen.getByText("Activating your subscription"),
      ).toBeInTheDocument();
      expect(screen.queryByText("Pro is active")).not.toBeInTheDocument();
      expect(screen.queryByText("Unlimited")).not.toBeInTheDocument();
      expect(screen.getByText("11 / 17")).toBeInTheDocument();
      expect(screen.getByText("5 / 7")).toBeInTheDocument();
      const summaryOptions = mocks.useQuery.mock.calls
        .map(([options]) => options)
        .find(
          (options) =>
            options.queryKey?.[options.queryKey.length - 1] === "summary",
        );
      expect(
        summaryOptions.refetchInterval({ state: { data: mocks.summary } }),
      ).toBe(2_000);
    } finally {
      vi.useRealTimers();
    }
  });

  it("confirms Checkout from refreshed Cloud actions and limit modes", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2030-01-01T00:00:00Z"));
    navigationState.search = "tab=billing&result=success&session_id=cs_test_1";
    Object.assign(mocks.entitlements, {
      plan: "pro",
      status: "active",
      limits: {
        issueCount: { mode: "unlimited", limit: null },
        autopilotRuns: { mode: "unlimited", limit: null },
      },
    });
    mocks.summary.availableActions.checkout = false;
    mocks.summaryDataUpdatedAt = Date.now() + 1;
    mocks.summaryFetchedAfterMount = true;
    try {
      renderWithI18n(<BillingTab />);

      expect(screen.getByText("Pro is active")).toBeInTheDocument();
      expect(screen.getAllByText("Unlimited")).toHaveLength(2);
      expect(screen.queryByText("5 / 7")).not.toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });

  it("keeps plan facts visible but hides mutations denied by Cloud", () => {
    Object.assign(mocks.summary.availableActions, {
      checkout: false,
      portal: false,
      purchaseSeats: false,
    });

    renderWithI18n(<BillingTab />);

    expect(screen.getAllByText("3 members")).toHaveLength(1);
    expect(screen.queryByText("Purchased seats")).not.toBeInTheDocument();
    expect(screen.queryByText("$10.00 per human seat")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Upgrade to Pro" }),
    ).not.toBeInTheDocument();
  });

  it("renders authoritative subscription seat facts", () => {
    Object.assign(mocks.entitlements, {
      plan: "pro",
      status: "active",
      currentPeriodEnd: "2030-02-01T00:00:00Z",
    });
    Object.assign(mocks.usage, {
      action: "off",
      used: null,
      reserved: null,
      limit: null,
      reset_at: null,
    });
    Object.assign(mocks.summary, {
      billingInterval: "month",
      hasStripeCustomer: true,
    });
    setSeatCapacity({
      humanMembers: 4,
      purchased: 5,
      used: 4,
      reserved: 1,
      available: 0,
      pendingQuantity: 4,
    });

    renderWithI18n(<BillingTab />);

    expect(screen.getByText("Monthly")).toBeInTheDocument();
    expect(screen.getByText("5 seats")).toBeInTheDocument();
    expect(screen.getByText("4 seats")).toBeInTheDocument();
    expect(screen.getByText("1 seat")).toBeInTheDocument();
    expect(screen.getByText("0 seats")).toBeInTheDocument();
    expect(screen.getByText(/4 seats from Feb 1, 2030/)).toBeInTheDocument();
    expect(screen.getAllByText("4 members")).toHaveLength(1);
  });

  it("quotes and confirms an additive seat purchase", async () => {
    const user = userEvent.setup();
    Object.assign(mocks.entitlements, {
      plan: "pro",
      status: "active",
    });
    mocks.summary.hasStripeCustomer = true;
    setSeatCapacity();
    mocks.previewSeats.mockImplementation(
      ({ additionalSeats }: { additionalSeats: number }) =>
        Promise.resolve({
          currentSeats: 5,
          additionalSeats,
          resultingSeats: 5 + additionalSeats,
          purchaseVersion: 9,
          currency: "usd",
          prorationAmount: 250 * additionalSeats,
          nextInvoiceAmount: 1000 * (5 + additionalSeats),
          quotedAt: "2030-01-01T00:00:00Z",
        }),
    );
    mocks.purchaseSeats.mockResolvedValue({
      requestId: "seat-request-2",
      currentSeats: 5,
      additionalSeats: 2,
      resultingSeats: 7,
      currency: "usd",
      prorationAmount: 500,
      nextInvoiceAmount: 7000,
      status: "submitted",
    });

    renderWithI18n(<BillingTab />);
    await user.click(screen.getByRole("button", { name: "Add seats" }));

    await waitFor(() =>
      expect(mocks.previewSeats).toHaveBeenCalledWith({ additionalSeats: 1 }),
    );
    expect(screen.getByText("Estimated charge today")).toBeInTheDocument();
    expect(screen.getByText("$2.50")).toBeInTheDocument();
    expect(screen.getByText("$60.00")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Add 1 seat" }),
    ).toBeInTheDocument();

    const input = screen.getByRole("spinbutton", { name: "Additional seats" });
    await user.clear(input);
    await user.type(input, "2");
    await waitFor(() =>
      expect(mocks.previewSeats).toHaveBeenLastCalledWith({
        additionalSeats: 2,
      }),
    );
    await waitFor(() => expect(screen.getByText("$5.00")).toBeInTheDocument());

    await user.click(screen.getByRole("button", { name: "Add 2 seats" }));
    expect(mocks.purchaseSeats).toHaveBeenCalledWith(
      expect.objectContaining({
        additionalSeats: 2,
        expectedCurrentSeats: 5,
        expectedPurchaseVersion: 9,
        acceptedProrationAmount: 500,
        currency: "usd",
        idempotencyKey: expect.stringContaining(
          "workspace-seat-purchase-workspace-1-",
        ),
      }),
    );
    await waitFor(() => expect(mocks.refetchSummary).toHaveBeenCalled());
  });

  it("refreshes and retries a preview after Stripe seat capacity changes", async () => {
    const user = userEvent.setup();
    let resolveSummaryRefresh!: (value: { data: typeof mocks.summary }) => void;
    Object.assign(mocks.entitlements, {
      plan: "pro",
      status: "active",
    });
    mocks.summary.hasStripeCustomer = true;
    setSeatCapacity();
    mocks.refetchSummary.mockReturnValueOnce(
      new Promise((resolve) => {
        resolveSummaryRefresh = resolve;
      }),
    );
    mocks.previewSeats
      .mockRejectedValueOnce(
        new ApiError("conflict", 409, "Conflict", {
          code: "seat_capacity_changed",
        }),
      )
      .mockResolvedValueOnce({
        currentSeats: 4,
        additionalSeats: 1,
        resultingSeats: 5,
        purchaseVersion: 10,
        currency: "usd",
        prorationAmount: 250,
        nextInvoiceAmount: 5000,
        quotedAt: "2030-01-01T00:00:00Z",
      });

    renderWithI18n(<BillingTab />);
    await user.click(screen.getByRole("button", { name: "Add seats" }));

    await waitFor(() => expect(mocks.refetchSummary).toHaveBeenCalledTimes(1));
    expect(screen.getByRole("status")).toHaveTextContent(
      "Updating estimate...",
    );
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();

    await act(async () => {
      setSeatCapacity({ purchased: 4, version: 10 });
      resolveSummaryRefresh({ data: mocks.summary });
    });

    await waitFor(
      () => expect(mocks.previewSeats).toHaveBeenCalledTimes(2),
      { timeout: 3_000 },
    );
    expect(await screen.findByText("$2.50")).toBeInTheDocument();
    expect(
      screen.queryByText(
        "The estimate is temporarily unavailable. Retry in a moment.",
      ),
    ).not.toBeInTheDocument();
  });

  it("does not retry when the refreshed seat summary is unchanged", async () => {
    const user = userEvent.setup();
    Object.assign(mocks.entitlements, {
      plan: "pro",
      status: "active",
    });
    mocks.summary.hasStripeCustomer = true;
    setSeatCapacity();
    mocks.previewSeats.mockRejectedValueOnce(
      new ApiError("conflict", 409, "Conflict", {
        code: "seat_capacity_changed",
      }),
    );

    renderWithI18n(<BillingTab />);
    await user.click(screen.getByRole("button", { name: "Add seats" }));

    expect(
      await screen.findByText(
        "The seat count in Stripe does not match the billing record. Contact support before retrying this purchase.",
        {},
        { timeout: 3_000 },
      ),
    ).toBeInTheDocument();
    expect(mocks.previewSeats).toHaveBeenCalledTimes(1);
    expect(mocks.refetchSummary).toHaveBeenCalledTimes(1);
  });

  it("keeps a summary refresh failure distinct from a seat mismatch", async () => {
    const user = userEvent.setup();
    Object.assign(mocks.entitlements, {
      plan: "pro",
      status: "active",
    });
    mocks.summary.hasStripeCustomer = true;
    setSeatCapacity();
    mocks.refetchSummary.mockResolvedValueOnce({
      data: mocks.summary,
      isError: true,
    });
    mocks.previewSeats.mockRejectedValueOnce(
      new ApiError("conflict", 409, "Conflict", {
        code: "seat_capacity_changed",
      }),
    );

    renderWithI18n(<BillingTab />);
    await user.click(screen.getByRole("button", { name: "Add seats" }));

    expect(
      await screen.findByText(
        "The estimate is temporarily unavailable. Retry in a moment.",
        {},
        { timeout: 3_000 },
      ),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(
        "The seat count in Stripe does not match the billing record. Contact support before retrying this purchase.",
      ),
    ).not.toBeInTheDocument();
    expect(mocks.previewSeats).toHaveBeenCalledTimes(1);
    expect(mocks.refetchSummary).toHaveBeenCalledTimes(1);
  });

  it("does not refresh again when the retried preview still conflicts", async () => {
    const user = userEvent.setup();
    Object.assign(mocks.entitlements, {
      plan: "pro",
      status: "active",
    });
    mocks.summary.hasStripeCustomer = true;
    setSeatCapacity();
    mocks.refetchSummary.mockImplementationOnce(async () => {
      setSeatCapacity({ purchased: 4, version: 10 });
      return { data: mocks.summary };
    });
    mocks.previewSeats.mockRejectedValue(
      new ApiError("conflict", 409, "Conflict", {
        code: "seat_capacity_changed",
      }),
    );

    renderWithI18n(<BillingTab />);
    await user.click(screen.getByRole("button", { name: "Add seats" }));

    expect(
      await screen.findByText(
        "The seat count in Stripe does not match the billing record. Contact support before retrying this purchase.",
        {},
        { timeout: 3_000 },
      ),
    ).toBeInTheDocument();
    expect(mocks.previewSeats).toHaveBeenCalledTimes(2);
    expect(mocks.refetchSummary).toHaveBeenCalledTimes(1);
  });

  it.each([
    [
      "seat_quote_changed",
      "The seat count or estimate changed. Review the refreshed quote before confirming again.",
    ],
    [
      "seat_purchase_in_progress",
      "Another seat purchase is being processed. Wait for it to finish; contact support if it remains here.",
    ],
  ])("maps purchase conflict %s to actionable copy", async (code, copy) => {
    const user = userEvent.setup();
    Object.assign(mocks.entitlements, {
      plan: "pro",
      status: "active",
    });
    mocks.summary.hasStripeCustomer = true;
    setSeatCapacity();
    mocks.purchaseSeats.mockRejectedValue(
      new ApiError("conflict", 409, "Conflict", { code }),
    );

    renderWithI18n(<BillingTab />);
    await user.click(screen.getByRole("button", { name: "Add seats" }));
    await screen.findByRole("button", { name: "Add 1 seat" });
    await user.click(screen.getByRole("button", { name: "Add 1 seat" }));

    expect(await screen.findByText(copy)).toBeInTheDocument();
    expect(mocks.refetchSummary).toHaveBeenCalled();
    if (code === "seat_quote_changed") {
      await waitFor(() => expect(mocks.previewSeats).toHaveBeenCalledTimes(2));
    } else {
      expect(mocks.previewSeats).toHaveBeenCalledTimes(1);
    }
  });

  it("keeps the seat dialog open while purchase submission is pending", async () => {
    const user = userEvent.setup();
    Object.assign(mocks.entitlements, {
      plan: "pro",
      status: "active",
    });
    mocks.summary.hasStripeCustomer = true;
    setSeatCapacity();

    const { rerender } = renderWithI18n(<BillingTab />);
    await user.click(screen.getByRole("button", { name: "Add seats" }));
    await screen.findByRole("button", { name: "Add 1 seat" });

    mocks.purchasePending = true;
    rerender(<BillingTab />);
    await user.keyboard("{Escape}");
    expect(screen.getByRole("dialog", { name: "Add seats" })).toBeInTheDocument();

    mocks.purchasePending = false;
    rerender(<BillingTab />);
    await user.keyboard("{Escape}");
    await waitFor(() =>
      expect(screen.queryByRole("dialog", { name: "Add seats" })).not.toBeInTheDocument(),
    );
  });

  it("reuses an unreadable purchase intent after closing and reopening", async () => {
    const user = userEvent.setup();
    Object.assign(mocks.entitlements, {
      plan: "pro",
      status: "active",
    });
    mocks.summary.hasStripeCustomer = true;
    setSeatCapacity();
    mocks.purchaseSeats.mockResolvedValue(null);

    renderWithI18n(<BillingTab />);
    await user.click(screen.getByRole("button", { name: "Add seats" }));
    await screen.findByRole("button", { name: "Add 1 seat" });
    await user.click(screen.getByRole("button", { name: "Add 1 seat" }));
    expect(
      await screen.findByText(
        "The purchase response could not be read. Retry this quote; the same purchase request will be reused.",
      ),
    ).toBeInTheDocument();
    const firstKey = mocks.purchaseSeats.mock.calls[0]?.[0].idempotencyKey;

    await user.click(screen.getByRole("button", { name: "Cancel" }));
    await user.click(screen.getByRole("button", { name: "Add seats" }));
    await user.click(
      await screen.findByRole("button", { name: "Add 1 seat" }),
    );

    expect(mocks.purchaseSeats).toHaveBeenCalledTimes(2);
    expect(mocks.purchaseSeats.mock.calls[1]?.[0].idempotencyKey).toBe(firstKey);
    expect(mocks.previewSeats).toHaveBeenCalledTimes(1);
  });

  it("shows payment recovery copy and releases the local intent on 402", async () => {
    const user = userEvent.setup();
    Object.assign(mocks.entitlements, {
      plan: "pro",
      status: "active",
    });
    mocks.summary.hasStripeCustomer = true;
    setSeatCapacity();
    mocks.purchaseSeats.mockRejectedValue(
      new ApiError("payment failed", 402, "Payment Required", {
        code: "seat_purchase_payment_failed",
      }),
    );

    renderWithI18n(<BillingTab />);
    await user.click(screen.getByRole("button", { name: "Add seats" }));
    await user.click(
      await screen.findByRole("button", { name: "Add 1 seat" }),
    );

    expect(
      await screen.findByText(
        "Payment could not be completed. Update the payment method in Stripe, then request a new quote.",
      ),
    ).toBeInTheDocument();
  });

  it("stops seat purchase polling after two minutes", () => {
    vi.useFakeTimers();
    Object.assign(mocks.entitlements, { plan: "pro", status: "active" });
    setSeatCapacity({
      activePurchase: {
        requestId: "seat-request-pending",
        targetSeats: 7,
        status: "pending",
        expiresAt: "2030-01-01T00:15:00Z",
      },
    });
    try {
      renderWithI18n(<BillingTab />);
      expect(screen.getByText("Seat purchase processing")).toBeInTheDocument();

      act(() => vi.advanceTimersByTime(2 * 60_000));

      expect(
        screen.getByText("Seat confirmation is taking longer than expected"),
      ).toBeInTheDocument();
      const formattedExpiry = new Intl.DateTimeFormat("en", {
        dateStyle: "medium",
        timeStyle: "short",
      }).format(new Date("2030-01-01T00:15:00Z"));
      expect(
        screen.getByText(
          `Automatic checking has stopped. If the attempt is still pending after ${formattedExpiry}, reload this page before requesting another quote; contact support if it remains pending.`,
        ),
      ).toBeInTheDocument();
      const summaryOptions = mocks.useQuery.mock.calls
        .map(([options]) => options)
        .filter(
          (options) =>
            options.queryKey?.[options.queryKey.length - 1] === "summary",
        )
        .at(-1);
      expect(
        summaryOptions.refetchInterval({ state: { data: mocks.summary } }),
      ).toBe(false);
    } finally {
      vi.useRealTimers();
    }
  });

  it("does not promise an unlock time for a submitted seat purchase", () => {
    vi.useFakeTimers();
    Object.assign(mocks.entitlements, { plan: "pro", status: "active" });
    setSeatCapacity({
      activePurchase: {
        requestId: "seat-request-submitted",
        targetSeats: 7,
        status: "submitted",
        expiresAt: null,
      },
    });
    try {
      renderWithI18n(<BillingTab />);
      act(() => vi.advanceTimersByTime(2 * 60_000));

      expect(
        screen.getByText(
          "Automatic checking has stopped. The purchase may still complete; check again later or contact support before starting another purchase.",
        ),
      ).toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });

  it("renders Cloud's completed, reserved, total, and reached facts", () => {
    Object.assign(mocks.usage, {
      used: 5,
      reserved: 2,
      total: 7,
      limit: 7,
      reached: true,
    });

    renderWithI18n(<BillingTab />);

    expect(screen.getByText("Limit reached")).toBeInTheDocument();
    expect(screen.getByText("7 / 7")).toBeInTheDocument();
    expect(screen.getByText("5 completed · 2 in progress")).toBeInTheDocument();
  });

  it("does not render missing limited usage as zero or unlimited", async () => {
    const user = userEvent.setup();
    Object.assign(mocks.usage, {
      action: "off",
      used: null,
      reserved: null,
      limit: null,
      reset_at: null,
    });

    renderWithI18n(<BillingTab />);

    expect(
      screen.getByText("Usage is temporarily unavailable"),
    ).toBeInTheDocument();
    expect(screen.getByText("7 / month")).toBeInTheDocument();
    expect(screen.queryByText("Unlimited")).not.toBeInTheDocument();
    expect(screen.queryByText(/0 \/ 7/)).not.toBeInTheDocument();

    await user.click(
      screen.getByRole("button", { name: "Retry automation usage" }),
    );
    expect(mocks.refetchUsage).toHaveBeenCalledOnce();
  });

  it.each([
    ["request fails", true, false],
    ["response is malformed", false, true],
  ])(
    "fails closed when the authoritative subscription summary %s",
    (_case, isError, isMalformed) => {
      mocks.summaryError = isError;
      mocks.summaryMalformed = isMalformed;

      renderWithI18n(<BillingTab />);

      expect(screen.getByText("Billing is temporarily unavailable")).toBeInTheDocument();
      expect(screen.queryByText("Free")).not.toBeInTheDocument();
      expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
    },
  );

  it.each([
    ["incomplete", "Subscription setup is incomplete"],
    ["incomplete_expired", "Subscription setup expired"],
    ["paused", "Subscription is paused"],
    ["unpaid", "Subscription is unpaid"],
    ["canceled", "Subscription is canceled"],
  ])("renders a recovery notice for %s", (status, title) => {
    mocks.entitlements.status = status;

    renderWithI18n(<BillingTab />);

    expect(screen.getByText(title)).toBeInTheDocument();
  });

  it("shows grace and scheduled-cancellation dates from subscription facts", () => {
    Object.assign(mocks.entitlements, {
      plan: "pro",
      status: "past_due",
      currentPeriodEnd: "2030-03-01T00:00:00Z",
      snapshotExpiresAt: "2030-02-15T00:00:00Z",
    });
    Object.assign(mocks.summary, {
      graceUntil: "2030-02-15T00:00:00Z",
      cancelAtPeriodEnd: true,
      hasStripeCustomer: true,
    });
    setSeatCapacity();

    renderWithI18n(<BillingTab />);

    expect(
      screen.getByText(/Update your payment method by Feb 15, 2030/),
    ).toBeInTheDocument();
    expect(screen.getByText("Cancellation is scheduled")).toBeInTheDocument();
    expect(
      screen.getByText(/subscription is scheduled to cancel on Mar 1, 2030/),
    ).toBeInTheDocument();
  });

  it("renders Cloud-resolved Free access without deriving from past_due", () => {
    Object.assign(mocks.entitlements, {
      plan: "free",
      status: "past_due",
    });
    Object.assign(mocks.summary, {
      graceUntil: null,
      availableActions: {
        checkout: false,
        portal: true,
        purchaseSeats: false,
      },
    });

    renderWithI18n(<BillingTab />);

    expect(screen.queryByText("Unlimited")).not.toBeInTheDocument();
    expect(screen.getByText("11 / 17")).toBeInTheDocument();
    expect(screen.getByText("5 / 7")).toBeInTheDocument();
    expect(
      screen.getByText(
        "Open the Billing Portal to update your payment method. The plan badge above shows the access currently available.",
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText(/keep Pro access/)).not.toBeInTheDocument();
  });

  it("keeps cancellation and pending-seat dates on one summary snapshot", () => {
    Object.assign(mocks.entitlements, {
      plan: "pro",
      status: "active",
      currentPeriodEnd: "2030-03-01T00:00:00Z",
    });
    Object.assign(mocks.summary, {
      entitlement: {
        ...mocks.entitlements,
        currentPeriodEnd: "2030-04-01T00:00:00Z",
      },
      cancelAtPeriodEnd: true,
      hasStripeCustomer: true,
    });
    setSeatCapacity({ pendingQuantity: 4 });

    renderWithI18n(<BillingTab />);

    expect(
      screen.getByText(/subscription is scheduled to cancel on Apr 1, 2030/),
    ).toBeInTheDocument();
    expect(screen.getByText(/4 seats from Apr 1, 2030/)).toBeInTheDocument();
    expect(screen.getByText("Apr 1, 2030")).toBeInTheDocument();
    expect(screen.queryByText("Mar 1, 2030")).not.toBeInTheDocument();
    expect(
      screen.queryByText(/subscription is scheduled to cancel on Mar 1, 2030/),
    ).not.toBeInTheDocument();
    expect(screen.queryByText(/Pro remains available/)).not.toBeInTheDocument();
  });

  it("shows Cloud's explicit unlimited limits on a normal load", () => {
    Object.assign(mocks.entitlements, {
      plan: "pro",
      status: "active",
      limits: {
        issueCount: { mode: "unlimited", limit: null },
        autopilotRuns: { mode: "unlimited", limit: null },
      },
      currentPeriodEnd: "2026-09-13T00:00:00Z",
      version: 3,
    });
    mocks.summary.hasStripeCustomer = true;
    setSeatCapacity({ humanMembers: 3, purchased: 3 });

    renderWithI18n(<BillingTab />);

    expect(
      screen.getByRole("button", { name: "Manage billing" }),
    ).toBeInTheDocument();
    expect(screen.getAllByText("Unlimited")).toHaveLength(2);
    expect(screen.queryByText("5 / 7")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Upgrade to Pro" }),
    ).not.toBeInTheDocument();
    expect(mocks.useQuery).toHaveBeenCalledWith(
      expect.objectContaining({
        queryKey: ["workspace-subscriptions", "workspace-1", "prices"],
        enabled: false,
      }),
    );
  });

  it("warns when actual members exceed purchased seats", () => {
    Object.assign(mocks.entitlements, {
      plan: "pro",
      status: "active",
      version: 3,
    });
    setSeatCapacity({ humanMembers: 5, purchased: 4, used: 5 });

    renderWithI18n(<BillingTab />);

    expect(screen.getByText("Members exceed purchased seats")).toBeInTheDocument();
    expect(
      screen.getByText(
        "This workspace has 5 members but only 4 purchased seats. Add enough seats or remove members before sending more invitations.",
      ),
    ).toBeInTheDocument();
  });

  it("does not expose a transient used-seat ledger delay to customers", () => {
    Object.assign(mocks.entitlements, {
      plan: "pro",
      status: "active",
      version: 3,
    });
    setSeatCapacity({ humanMembers: 5, purchased: 5, used: 5 });

    renderWithI18n(<BillingTab />);

    expect(
      screen.queryByText("Members exceed purchased seats"),
    ).not.toBeInTheDocument();
  });

  it.each(["canceled", "incomplete_expired"])(
    "keeps management and self-service repurchase available for %s Free",
    (status) => {
      Object.assign(mocks.entitlements, {
        plan: "free",
        status,
        currentPeriodEnd: "2026-09-13T00:00:00Z",
        version: 4,
      });
      Object.assign(mocks.summary, {
        seatCapacity: null,
        hasStripeCustomer: true,
        availableActions: {
          checkout: true,
          portal: true,
          purchaseSeats: false,
        },
      });

      renderWithI18n(<BillingTab />);

      expect(
        screen.getByRole("button", { name: "Manage billing" }),
      ).toBeInTheDocument();
      expect(
        screen.getByRole("button", { name: "Upgrade to Pro" }),
      ).toBeInTheDocument();
      expect(screen.queryByText("Purchased seats")).not.toBeInTheDocument();
      expect(mocks.useQuery).toHaveBeenCalledWith(
        expect.objectContaining({
          queryKey: ["workspace-subscriptions", "workspace-1", "prices"],
          enabled: true,
        }),
      );
    },
  );

  it("keeps payment recovery available when past-due grace has resolved to Free", () => {
    Object.assign(mocks.entitlements, {
      plan: "free",
      status: "past_due",
      snapshotExpiresAt: null,
      version: 4,
    });
    Object.assign(mocks.summary, {
      graceUntil: null,
      hasStripeCustomer: true,
      availableActions: {
        checkout: false,
        portal: true,
        purchaseSeats: false,
      },
    });

    renderWithI18n(<BillingTab />);

    expect(screen.getByText("Payment needs attention")).toBeInTheDocument();
    expect(
      screen.getByText(
        "Open the Billing Portal to update your payment method. The plan badge above shows the access currently available.",
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText(/keep Pro access/)).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Manage billing" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Upgrade to Pro" }),
    ).not.toBeInTheDocument();
  });
});
