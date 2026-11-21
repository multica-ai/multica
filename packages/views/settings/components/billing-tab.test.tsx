import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@multica/core/api";
import { configStore } from "@multica/core/config";
import { BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG } from "@multica/core/feature-flags";
import type {
  AutopilotQuotaUsage,
  MemberLimit,
  StorageUsage,
  WorkspaceSubscriptionEntitlements,
  WorkspaceSubscriptionPrices,
  WorkspaceSubscriptionSummary,
} from "@multica/core/types";
import {
  bindUpgradeInviteCheckout,
  readUpgradeInvite,
  saveUpgradeInvite,
} from "@multica/core/billing";
import { renderWithI18n } from "../../test/i18n";

const mocks = vi.hoisted(() => ({
  useQuery: vi.fn(),
  checkout: vi.fn(),
  portal: vi.fn(),
  previewSeats: vi.fn(),
  purchaseSeats: vi.fn(),
  previewPlanChange: vi.fn(),
  changePlan: vi.fn(),
  checkPlanChange: vi.fn(),
  changePlanPending: false,
  refetch: vi.fn(),
  refetchSummary: vi.fn(),
  refetchUsage: vi.fn(),
  refetchPrices: vi.fn(),
  openExternal: vi.fn(),
  workspaceId: "workspace-1",
  invalidateQueries: vi.fn(() => Promise.resolve()),
  memberLimit: { action: "off", limit: null, members: null, pendingInvitations: null } as MemberLimit,
  storageUsage: { action: "off", usedBytes: null, limitBytes: null, reached: null } as StorageUsage,
  refetchStorage: vi.fn(),
  prices: null as WorkspaceSubscriptionPrices | null,
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
      changePlan: false,
      checkPlanChange: false,
      purchaseAutopilotRuns: false,
    },
    autopilotAddon: null,
    planChange: null as WorkspaceSubscriptionSummary["planChange"],
    trial: null as WorkspaceSubscriptionSummary["trial"],
  } as WorkspaceSubscriptionSummary,
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
  useQueryClient: () => ({ invalidateQueries: mocks.invalidateQueries }),
  queryOptions: <T,>(options: T) => options,
}));

vi.mock("@multica/core/billing", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/billing")>()),
  workspaceSubscriptionPricesOptions: (wsId: string) => ({
    queryKey: ["workspace-subscriptions", wsId, "prices"],
  }),
  workspaceSubscriptionSummaryOptions: (wsId: string) => ({
    queryKey: ["workspace-subscriptions", wsId, "summary"],
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
  usePreviewWorkspacePlanChange: () => ({
    mutateAsync: mocks.previewPlanChange,
    isPending: false,
  }),
  useChangeWorkspacePlan: () => ({
    mutateAsync: mocks.changePlan,
    isPending: mocks.changePlanPending,
  }),
  useReconcileWorkspaceSubscriptionSeats: () => ({
    mutateAsync: mocks.checkPlanChange,
    isPending: false,
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
  push: vi.fn(),
};
vi.mock("../../navigation", () => ({
  useNavigation: () => ({
    pathname: "/acme/settings",
    searchParams: new URLSearchParams(navigationState.search),
    hash: "",
    push: navigationState.push,
    replace: navigationState.replace,
    back: vi.fn(),
    getShareableUrl: vi.fn(),
  }),
}));

vi.mock("../../platform", () => ({ openExternal: mocks.openExternal }));

vi.mock("../../billing/autopilot-addon-dialog", () => ({
  AutopilotAddonDialog: ({ open }: { open: boolean }) =>
    open ? <div role="dialog" aria-label="Buy more automation runs" /> : null,
}));

vi.mock("../../billing/autopilot-addon-return-notice", () => ({
  AutopilotAddonReturnNotice: ({ sessionId }: { sessionId: string | null }) => (
    <div role="status" aria-label="Add-on return">{sessionId}</div>
  ),
}));

import { BillingTab, formatStripeMinorAmount } from "./billing-tab";

// The component formats dates in the viewer's own time zone, so a midnight-UTC
// fixture reads as the previous calendar day anywhere behind UTC. Build the
// expected text with the same formatter instead of hard-coding the calendar day.
function formatExpectedDate(iso: string): string {
  return new Intl.DateTimeFormat("en", { dateStyle: "medium" }).format(
    new Date(iso),
  );
}

const FEB_1_2030 = formatExpectedDate("2030-02-01T00:00:00Z");
const FEB_15_2030 = formatExpectedDate("2030-02-15T00:00:00Z");
const MAR_1_2030 = formatExpectedDate("2030-03-01T00:00:00Z");
const APR_1_2030 = formatExpectedDate("2030-04-01T00:00:00Z");

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
      pro: {
        month: {
          currency: "usd",
          unitAmount: 1500,
          interval: "month",
          intervalCount: 1,
        },
        year: {
          currency: "usd",
          unitAmount: 14400,
          interval: "year",
          intervalCount: 1,
        },
      },
      business: {
        month: {
          currency: "usd",
          unitAmount: 3000,
          interval: "month",
          intervalCount: 1,
        },
        year: {
          currency: "usd",
          unitAmount: 28800,
          interval: "year",
          intervalCount: 1,
        },
      },
      autopilotAddon: null,
    };
    mocks.changePlanPending = false;
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
        changePlan: false,
        checkPlanChange: false,
        purchaseAutopilotRuns: false,
      },
      autopilotAddon: null,
      planChange: null,
      trial: null,
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
    localStorage.clear();
    mocks.memberLimit = { action: "off", limit: null, members: null, pendingInvitations: null };
    mocks.storageUsage = { action: "off", usedBytes: null, limitBytes: null, reached: null };
    mocks.useQuery.mockImplementation((options: unknown) => {
      const queryKey = (options as { queryKey?: readonly unknown[] }).queryKey;
      if (queryKey?.[0] === "workspace-storage-usage") {
        return { data: mocks.storageUsage, isPending: false, isError: false, refetch: mocks.refetchStorage };
      }
      if (queryKey?.[queryKey.length - 1] === "limit") {
        return { data: mocks.memberLimit, isPending: false, isError: false };
      }
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
    mocks.previewPlanChange.mockResolvedValue({
      currentPlan: "pro",
      targetPlan: "business",
      interval: "month",
      seats: 4,
      subscriptionVersion: 7,
      currency: "usd",
      prorationAmount: 3000,
      nextInvoiceAmount: 12000,
      invoiceNow: true,
      quotedAt: "2030-01-15T00:00:00Z",
    });
    mocks.changePlan.mockResolvedValue({
      plan: "business",
      previousPlan: "pro",
      interval: "month",
      seats: 4,
      currency: "usd",
      prorationAmount: 3000,
      invoiceNow: true,
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

  it("shows the plan's analytics history range only when Cloud reports it", () => {
    const { unmount } = renderWithI18n(<BillingTab />);
    expect(screen.queryByText("Analytics history")).toBeNull();
    unmount();

    mocks.entitlements.limits = {
      ...mocks.entitlements.limits,
      dashboardHistoryDays: { mode: "limited", limit: 30 },
    };
    renderWithI18n(<BillingTab />);
    expect(screen.getByText("Analytics history")).toBeInTheDocument();
    expect(screen.getByText("Last 30 days")).toBeInTheDocument();
  });

  it("shows server prices and keeps the selected interval in sync", async () => {
    const user = userEvent.setup();
    renderWithI18n(<BillingTab />);

    expect(screen.getByRole("heading", { name: "Billing" })).toBeInTheDocument();
    expect(screen.getByText("5 / 7")).toBeInTheDocument();
    expect(screen.getByText("3 completed · 2 in progress")).toBeInTheDocument();
    expect(
      screen.getByText("$15.00 per human seat / month"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("$30.00 per human seat / month"),
    ).toBeInTheDocument();
    expect(mocks.useQuery).toHaveBeenCalledWith(
      expect.objectContaining({
        queryKey: ["workspace-subscriptions", "workspace-1", "prices"],
      }),
    );

    await user.click(screen.getByRole("button", { name: "Yearly" }));

    expect(
      screen.getByText("$12.00 per human seat / month, billed yearly"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("$24.00 per human seat / month, billed yearly"),
    ).toBeInTheDocument();
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

    expect(
      screen.getByText("$15.00 per human seat / month"),
    ).toBeInTheDocument();
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
      plan: string;
      interval: string;
      idempotencyKey: string;
    };
    expect(request.plan).toBe("pro");
    expect(request.interval).toBe("month");
    expect(request.idempotencyKey).toMatch(/^workspace-checkout-workspace-1-/);
    expect(Object.keys(request).sort()).toEqual([
      "idempotencyKey",
      "interval",
      "plan",
    ]);
    expect(mocks.openExternal).toHaveBeenCalledWith(
      "https://checkout.stripe.com/test-session",
      { webTarget: "same-tab" },
    );
  });

  describe("member limit", () => {
    const upgraded = () => {
      Object.assign(mocks.entitlements, { plan: "pro", status: "active", version: 1 });
      mocks.summary.availableActions.checkout = false;
      mocks.summaryDataUpdatedAt = Date.now() + 1;
      mocks.summaryFetchedAfterMount = true;
    };
    const boundHeldInvite = (email: string, requestId: string) => {
      const token = saveUpgradeInvite("acme", { email, role: "member" });
      bindUpgradeInviteCheckout("acme", token, { requestId, baseVersion: 0 });
      return token;
    };
    const membersResume = (token: string) =>
      `/acme/settings?tab=members&invite=resume&held_invite=${token}`;

    it("opens the Pro checkout confirmation when Members asks for an upgrade", async () => {
      navigationState.search = "tab=billing&upgrade=pro";
      renderWithI18n(<BillingTab />);

      const confirm = await screen.findByRole("alertdialog");
      expect(confirm).toHaveTextContent("Plan: Pro");
      expect(navigationState.replace).toHaveBeenCalledWith("/acme/settings?tab=billing");
    });

    it("buys a seat for the held invitation and binds it to that checkout", async () => {
      const token = saveUpgradeInvite("acme", { email: "sarah@acme.dev", role: "member" });
      navigationState.search = `tab=billing&upgrade=pro&held_invite=${token}`;
      const user = userEvent.setup();
      renderWithI18n(<BillingTab />);

      expect(await screen.findByRole("alertdialog")).toHaveTextContent(
        "One more paid seat is included for the invitation to sarah@acme.dev",
      );
      await user.click(screen.getByRole("button", { name: "Continue to Stripe" }));

      await waitFor(() => expect(mocks.checkout).toHaveBeenCalledTimes(1));
      expect(mocks.checkout.mock.calls[0]?.[0]).toMatchObject({ plan: "pro", additionalSeats: 1 });
      expect(readUpgradeInvite("acme", token)?.checkout).toMatchObject({ requestId: "request-1", baseVersion: 0 });
    });

    // Desktop: Stripe opens in the system browser, the person closes it and
    // upgrades again from the same page. That checkout is a plain one.
    it("makes the next checkout from the same page a plain one once the held one started", async () => {
      const token = saveUpgradeInvite("acme", { email: "sarah@acme.dev", role: "member" });
      navigationState.search = `tab=billing&upgrade=pro&held_invite=${token}`;
      const user = userEvent.setup();
      renderWithI18n(<BillingTab />);

      await screen.findByRole("alertdialog");
      await user.click(screen.getByRole("button", { name: "Continue to Stripe" }));
      await waitFor(() => expect(mocks.checkout).toHaveBeenCalledTimes(1));

      await user.click(screen.getByRole("button", { name: "Upgrade to Pro" }));
      expect(screen.getByRole("alertdialog")).not.toHaveTextContent("One more paid seat");
      await user.click(screen.getByRole("button", { name: "Continue to Stripe" }));
      await waitFor(() => expect(mocks.checkout).toHaveBeenCalledTimes(2));

      const [held, plain] = mocks.checkout.mock.calls.map(([request]) => request);
      expect(plain).not.toHaveProperty("additionalSeats");
      expect(plain.idempotencyKey).not.toBe(held.idempotencyKey);
    });

    it("drops a cancelled held invitation, so a later upgrade buys no seat for it", async () => {
      const token = saveUpgradeInvite("acme", { email: "sarah@acme.dev", role: "member" });
      navigationState.search = `tab=billing&upgrade=pro&held_invite=${token}`;
      const user = userEvent.setup();
      renderWithI18n(<BillingTab />);

      await screen.findByRole("alertdialog");
      await user.click(screen.getByRole("button", { name: "Cancel" }));
      expect(readUpgradeInvite("acme", token)).toBeNull();

      await user.click(screen.getByRole("button", { name: "Upgrade to Pro" }));
      await user.click(screen.getByRole("button", { name: "Continue to Stripe" }));
      await waitFor(() => expect(mocks.checkout).toHaveBeenCalledTimes(1));
      expect(mocks.checkout.mock.calls[0]?.[0]).not.toHaveProperty("additionalSeats");
    });

    it("leaves other tabs' held invitations alone when an upgrade starts without one", async () => {
      const token = boundHeldInvite("sarah@acme.dev", "req-tab-a");
      const user = userEvent.setup();
      renderWithI18n(<BillingTab />);

      await user.click(screen.getByRole("button", { name: "Upgrade to Pro" }));
      await user.click(screen.getByRole("button", { name: "Continue to Stripe" }));

      await waitFor(() => expect(mocks.checkout).toHaveBeenCalledTimes(1));
      expect(mocks.checkout.mock.calls[0]?.[0]).not.toHaveProperty("additionalSeats");
      expect(readUpgradeInvite("acme", token)?.checkout?.requestId).toBe("req-tab-a");
    });

    it("waits for the paid plan: an incomplete payment still reads as Free", () => {
      const token = boundHeldInvite("sarah@acme.dev", "req-held");
      navigationState.search = "tab=billing&result=success&session_id=cs_test_1";
      Object.assign(mocks.entitlements, { plan: "free", status: "incomplete", version: 1 });
      mocks.summary.availableActions.checkout = false;
      renderWithI18n(<BillingTab />);

      expect(navigationState.push).not.toHaveBeenCalled();
      expect(screen.queryByRole("button", { name: "Send invitation" })).not.toBeInTheDocument();
      expect(readUpgradeInvite("acme", token)).not.toBeNull();
    });

    // Desktop pays in the system browser and never sees the return; another
    // admin may also have upgraded from elsewhere. Neither proves this
    // invitation's checkout was paid.
    it("asks before sending when no return ties the paid plan to the held checkout", async () => {
      const token = boundHeldInvite("sarah@acme.dev", "req-unverified");
      upgraded();
      const user = userEvent.setup();
      renderWithI18n(<BillingTab />);

      expect(navigationState.push).not.toHaveBeenCalled();
      expect(screen.getByText("The workspace is on Pro now")).toBeInTheDocument();
      expect(
        screen.getByText("You held back an invitation for sarah@acme.dev as a member until the upgrade. Send it now?"),
      ).toBeInTheDocument();

      await user.click(screen.getByRole("button", { name: "Send invitation" }));
      expect(navigationState.push).toHaveBeenCalledWith(membersResume(token));
    });

    // Desktop: held checkout A, then plain checkout B, then A is paid in the
    // system browser. Nothing proves which was paid, so A stays on offer.
    it("keeps offering a held invitation a later plain checkout might have hidden", async () => {
      const token = boundHeldInvite("sarah@acme.dev", "req-a");
      const user = userEvent.setup();
      const view = renderWithI18n(<BillingTab />);
      await user.click(screen.getByRole("button", { name: "Upgrade to Pro" }));
      await user.click(screen.getByRole("button", { name: "Continue to Stripe" }));
      await waitFor(() => expect(mocks.checkout).toHaveBeenCalledTimes(1));
      view.unmount();

      upgraded();
      renderWithI18n(<BillingTab />);
      await user.click(screen.getByRole("button", { name: "Send invitation" }));
      expect(navigationState.push).toHaveBeenCalledWith(membersResume(token));
    });

    it("starts only plain checkouts from this page after a held checkout failed", async () => {
      const token = saveUpgradeInvite("acme", { email: "sarah@acme.dev", role: "member" });
      navigationState.search = `tab=billing&upgrade=pro&held_invite=${token}`;
      mocks.checkout.mockRejectedValueOnce(new Error("timeout creating the Stripe session"));
      const user = userEvent.setup();
      renderWithI18n(<BillingTab />);

      await screen.findByRole("alertdialog");
      await user.click(screen.getByRole("button", { name: "Continue to Stripe" }));
      await waitFor(() => expect(mocks.checkout).toHaveBeenCalledTimes(1));

      await user.click(screen.getByRole("button", { name: "Upgrade to Pro" }));
      expect(screen.getByRole("alertdialog")).not.toHaveTextContent("One more paid seat");
      await user.click(screen.getByRole("button", { name: "Continue to Stripe" }));
      await waitFor(() => expect(mocks.checkout).toHaveBeenCalledTimes(2));
      expect(mocks.checkout.mock.calls[1]?.[0]).not.toHaveProperty("additionalSeats");
    });

    it("keeps the upgrade intent and its invitation while the summary cannot be read", () => {
      const token = saveUpgradeInvite("acme", { email: "sarah@acme.dev", role: "admin" });
      navigationState.search = `tab=billing&upgrade=pro&held_invite=${token}`;
      mocks.summaryError = true;
      renderWithI18n(<BillingTab />);

      expect(navigationState.replace).not.toHaveBeenCalled();
      expect(readUpgradeInvite("acme", token)).toMatchObject({ email: "sarah@acme.dev", role: "admin" });
    });

    // Two tabs held A and B, B is newer; A's checkout is the one paid. Both
    // are offered, so the seat just bought can go to A.
    it("offers every paid-for held invitation so the person picks which to send", async () => {
      const a = boundHeldInvite("sarah@acme.dev", "req-a");
      boundHeldInvite("omar@acme.dev", "req-b");
      navigationState.search = "tab=billing&result=success&session_id=cs_test_1";
      upgraded();
      const user = userEvent.setup();
      renderWithI18n(<BillingTab />);

      expect(screen.getByText(/invitation for omar@acme\.dev/)).toBeInTheDocument();
      const sarah = screen.getByText(/invitation for sarah@acme\.dev/).closest("li")!;
      await user.click(within(sarah).getByRole("button", { name: "Send invitation" }));
      expect(navigationState.push).toHaveBeenCalledWith(membersResume(a));
    });

    it("discards a held invitation the person no longer wants", async () => {
      const token = boundHeldInvite("sarah@acme.dev", "req-unverified");
      upgraded();
      const user = userEvent.setup();
      renderWithI18n(<BillingTab />);

      await user.click(screen.getByRole("button", { name: "Discard" }));

      expect(readUpgradeInvite("acme", token)).toBeNull();
      expect(navigationState.push).not.toHaveBeenCalled();
      expect(screen.queryByRole("button", { name: "Send invitation" })).not.toBeInTheDocument();
    });

    const heldCheckoutPollInterval = () => {
      const summaryOptions = mocks.useQuery.mock.calls
        .map(([options]) => options)
        .findLast((options) => options.queryKey?.[options.queryKey.length - 1] === "summary");
      return summaryOptions.refetchInterval({ state: { data: mocks.summary } });
    };

    it("stops polling a held checkout after two minutes and offers the invitation after a focus refresh", () => {
      vi.useFakeTimers();
      try {
        const token = boundHeldInvite("sarah@acme.dev", "req-desktop");
        const view = renderWithI18n(<BillingTab />);
        expect(heldCheckoutPollInterval()).toBe(5_000);
        expect(screen.queryByRole("button", { name: "Send invitation" })).not.toBeInTheDocument();

        act(() => vi.advanceTimersByTime(120_000));
        expect(heldCheckoutPollInterval()).toBe(false);
        expect(readUpgradeInvite("acme", token)).not.toBeNull();

        act(() => window.dispatchEvent(new Event("focus")));
        expect(mocks.refetchSummary).toHaveBeenCalledWith({ cancelRefetch: false });
        upgraded();
        view.rerender(<BillingTab />);
        expect(screen.getByRole("button", { name: "Send invitation" })).toBeInTheDocument();
        expect(navigationState.push).not.toHaveBeenCalled();
      } finally {
        vi.useRealTimers();
      }
    });

    it("does not restart fast polling when Billing opens with an older held checkout", () => {
      vi.useFakeTimers();
      try {
        const token = boundHeldInvite("sarah@acme.dev", "req-old");
        vi.advanceTimersByTime(25 * 60 * 60_000);
        renderWithI18n(<BillingTab />);

        expect(heldCheckoutPollInterval()).toBe(false);
        expect(mocks.refetchSummary).toHaveBeenCalledOnce();
        expect(readUpgradeInvite("acme", token)).not.toBeNull();
      } finally {
        vi.useRealTimers();
      }
    });

    it("shows attachment storage against the plan's limit (MUL-9489)", () => {
      mocks.storageUsage = { action: "observe", usedBytes: 23_400_000_000, limitBytes: 120_000_000_000, reached: null };
      renderWithI18n(<BillingTab />);

      expect(screen.getByText("Attachment storage")).toBeInTheDocument();
      expect(screen.getByText("Attachments in issues, comments, and chat.")).toBeInTheDocument();
      expect(screen.getByText("23.4 GB / 120 GB")).toBeInTheDocument();
    });

    it("reports enforced full storage as reached, and hides storage without a limit", () => {
      mocks.storageUsage = { action: "enforce", usedBytes: 1_000_000_000, limitBytes: 1_000_000_000, reached: true };
      const { unmount } = renderWithI18n(<BillingTab />);
      expect(screen.getByText("1 GB / 1 GB")).toBeInTheDocument();
      expect(screen.getAllByText("Limit reached").length).toBeGreaterThan(0);
      unmount();

      mocks.storageUsage = { action: "off", usedBytes: null, limitBytes: null, reached: null };
      renderWithI18n(<BillingTab />);
      expect(screen.queryByText("Attachment storage")).not.toBeInTheDocument();
    });

    it("shows storage usage it could not read as unavailable, with a retry", async () => {
      const user = userEvent.setup();
      mocks.refetchStorage.mockReset();
      mocks.storageUsage = { action: "observe", usedBytes: null, limitBytes: 1_000_000_000, reached: null };
      renderWithI18n(<BillingTab />);

      expect(screen.getByText("Attachment storage")).toBeInTheDocument();
      expect(screen.getByText("1 GB")).toBeInTheDocument();
      await user.click(screen.getByRole("button", { name: "Retry attachment storage usage" }));
      expect(mocks.refetchStorage).toHaveBeenCalledOnce();
    });

    // After Pro moves to Business (or seats are added) the summary carries
    // the new limit at once, while the usage read can still hold the old one.
    it("shows the plan's new storage limit as soon as the subscription changes", () => {
      mocks.storageUsage = { action: "observe", usedBytes: 12_000_000_000, limitBytes: 30_000_000_000, reached: null };
      mocks.entitlements.limits = { ...mocks.entitlements.limits, storageBytes: { mode: "limited", limit: 30_000_000_000 } };
      const { rerender } = renderWithI18n(<BillingTab />);
      expect(screen.getByText("12 GB / 30 GB")).toBeInTheDocument();
      mocks.invalidateQueries.mockClear();

      mocks.entitlements.limits = { ...mocks.entitlements.limits, storageBytes: { mode: "limited", limit: 300_000_000_000 } };
      mocks.entitlements.version = 1;
      rerender(<BillingTab />);

      expect(screen.getByText("12 GB / 300 GB")).toBeInTheDocument();
      expect(mocks.invalidateQueries).toHaveBeenCalledWith({ queryKey: ["workspace-storage-usage", expect.any(String)] });
    });

    it("shows members and pending invitations against the plan's limit", () => {
      mocks.memberLimit = { action: "observe", limit: 5, members: 4, pendingInvitations: 2 };
      renderWithI18n(<BillingTab />);

      expect(screen.getByText("Members")).toBeInTheDocument();
      expect(screen.getByText("6 / 5")).toBeInTheDocument();
      expect(screen.getByText("Pending invitations: 2")).toBeInTheDocument();
    });
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

  it("explains that a disabled billing capability requires an administrator", async () => {
    const user = userEvent.setup();
    mocks.checkout.mockRejectedValue(
      new ApiError("request failed", 403, "Forbidden", {
        code: "workspace_subscriptions_disabled",
      }),
    );
    renderWithI18n(<BillingTab />);

    await user.click(screen.getByRole("button", { name: "Upgrade to Pro" }));
    await user.click(screen.getByRole("button", { name: "Continue to Stripe" }));

    expect(
      await screen.findByText(
        "Workspace subscriptions are not enabled for this deployment. Contact your administrator.",
      ),
    ).toBeInTheDocument();
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

  it("hands an add-on return's Checkout Session to the return notice after clearing the URL", () => {
    navigationState.search = "tab=billing&result=autopilot_addon&session_id=cs_test_7";

    renderWithI18n(<BillingTab />);

    expect(screen.getByRole("status", { name: "Add-on return" })).toHaveTextContent("cs_test_7");
    expect(navigationState.replace).toHaveBeenCalledWith("/acme/settings?tab=billing");
  });

  it("says nothing was charged when an add-on Checkout is canceled", () => {
    navigationState.search = "tab=billing&result=autopilot_addon_cancel";

    renderWithI18n(<BillingTab />);

    expect(screen.getByText("Payment canceled")).toBeInTheDocument();
    expect(screen.getByText("Nothing was charged.")).toBeInTheDocument();
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
        autopilotRuns: { mode: "unlimited", limit: null },
      },
    });
    setSeatCapacity({ humanMembers: 3, purchased: 3 });
    mocks.summaryDataUpdatedAt = Date.now() + 1;
    mocks.summaryFetchedAfterMount = true;
    try {
      renderWithI18n(<BillingTab />);

      expect(screen.getByText("Pro is active")).toBeInTheDocument();
      expect(screen.getByText("Unlimited")).toBeInTheDocument();
      expect(screen.queryByText("5 / 7")).not.toBeInTheDocument();
      expect(screen.queryByText(/Runs a member starts with Run now/)).not.toBeInTheDocument();
      // The paid plan is recorded: paid features and plan limits are re-read.
      expect(mocks.invalidateQueries).toHaveBeenCalledWith({
        queryKey: ["workspace-features", "workspace-1"],
      });
    } finally {
      vi.useRealTimers();
    }
  });

  it("keeps syncing a checkout started during a trial until paid seats take effect", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2030-01-01T00:00:00Z"));
    navigationState.search = "tab=billing&result=success&session_id=cs_test_1";
    // Still on the Business trial while the first Pro payment is incomplete:
    // the plan is paid and Checkout is gone, but nothing has been paid for.
    Object.assign(mocks.entitlements, {
      plan: "business",
      status: "trialing",
      limits: { autopilotRuns: { mode: "unlimited", limit: null } },
    });
    mocks.summary.trial = {
      plan: "business",
      state: "active",
      startedAt: "2029-12-25T00:00:00Z",
      endsAt: "2030-01-08T00:00:00Z",
    };
    mocks.summary.availableActions.checkout = false;
    mocks.summaryDataUpdatedAt = Date.now() + 1;
    mocks.summaryFetchedAfterMount = true;
    try {
      renderWithI18n(<BillingTab />);

      expect(screen.getByText("Activating your subscription")).toBeInTheDocument();
      expect(screen.queryByText("Business is active")).not.toBeInTheDocument();
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

  it("keeps plan facts visible but hides mutations denied by Cloud", () => {
    Object.assign(mocks.summary.availableActions, {
      checkout: false,
      portal: false,
      purchaseSeats: false,
    });

    renderWithI18n(<BillingTab />);

    expect(screen.getAllByText("3 members")).toHaveLength(1);
    expect(screen.queryByText("Purchased seats")).not.toBeInTheDocument();
    expect(screen.queryByText(/per human seat/)).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Upgrade to Pro" }),
    ).not.toBeInTheDocument();
  });

  it("renders authoritative subscription seat facts and opens their calculation help", async () => {
    const user = userEvent.setup();
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
    expect(
      screen.getByText(new RegExp(`4 seats from ${FEB_1_2030}`)),
    ).toBeInTheDocument();
    expect(screen.getAllByText("4 members")).toHaveLength(1);
    expect(screen.getByText("Available seats").closest("summary")).toBeNull();
    const formula = "Purchased seats minus members and reserved invitations.";
    expect(screen.queryByText(formula)).not.toBeInTheDocument();
    const help = screen.getByRole("button", { name: "How available seats are calculated" });
    help.focus();
    await user.keyboard("{Enter}");
    expect(await screen.findByText(formula)).toBeVisible();
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByText(formula)).not.toBeInTheDocument());
    expect(help).toHaveFocus();
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
    [
      "plan_change_in_progress",
      "A plan change is still being processed. Try again in a few minutes.",
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
    expect(
      screen.getByText(
        "Runs from schedules, webhooks, and agents count. Runs a member starts with Run now don't.",
      ),
    ).toBeInTheDocument();
  });

  it("shows this month's add-on runs and lets a manager buy more", async () => {
    Object.assign(mocks.entitlements, { plan: "pro", status: "active" });
    Object.assign(mocks.summary, {
      autopilotAddon: {
        purchasedRuns: 2000,
        periodStart: "2030-01-01T00:00:00Z",
        periodEnd: "2030-02-01T00:00:00Z",
      },
    });
    Object.assign(mocks.summary.availableActions, {
      checkout: false,
      purchaseAutopilotRuns: true,
    });
    mocks.prices = {
      ...mocks.prices!,
      autopilotAddon: { currency: "usd", unitAmount: 1000, runsPerPack: 1000 },
    };

    const user = userEvent.setup();
    renderWithI18n(<BillingTab />);

    expect(screen.getByText("Includes 2,000 add-on runs")).toBeInTheDocument();
    expect(screen.getByText("2,000 bought this month")).toBeInTheDocument();
    await user.click(
      screen.getByRole("button", { name: "Buy · $10.00 / 1,000 runs" }),
    );
    expect(
      await screen.findByRole("dialog", { name: "Buy more automation runs" }),
    ).toBeInTheDocument();
  });

  it("shows add-on runs without a purchase button to a member", () => {
    Object.assign(mocks.entitlements, { plan: "business", status: "active" });
    Object.assign(mocks.summary, {
      autopilotAddon: {
        purchasedRuns: 0,
        periodStart: "2030-01-01T00:00:00Z",
        periodEnd: "2030-02-01T00:00:00Z",
      },
    });
    Object.assign(mocks.summary.availableActions, {
      checkout: false,
      purchaseAutopilotRuns: false,
    });

    renderWithI18n(<BillingTab />);

    expect(screen.getByText("None this month")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /^Buy/ }),
    ).not.toBeInTheDocument();
  });

  it("hides add-on runs on a cloud that predates them", () => {
    renderWithI18n(<BillingTab />);

    expect(screen.queryByText("Add-on runs")).not.toBeInTheDocument();
  });

  it("shows observed usage over the limit without reporting it reached", () => {
    Object.assign(mocks.usage, {
      action: "observe",
      used: 9,
      reserved: 0,
      total: 9,
      limit: 7,
      reached: null,
    });

    renderWithI18n(<BillingTab />);

    expect(screen.getByText("9 / 7")).toBeInTheDocument();
    expect(screen.getByText("Current usage")).toBeInTheDocument();
    expect(screen.queryByText("Limit reached")).not.toBeInTheDocument();
    expect(
      screen.queryByText("Usage is temporarily unavailable"),
    ).not.toBeInTheDocument();
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
      screen.getByText(
        new RegExp(`Update your payment method by ${FEB_15_2030}`),
      ),
    ).toBeInTheDocument();
    expect(screen.getByText("Cancellation is scheduled")).toBeInTheDocument();
    expect(
      screen.getByText(new RegExp(`subscription will cancel on ${MAR_1_2030}`)),
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
    expect(screen.getByText("5 / 7")).toBeInTheDocument();
    expect(
      screen.getByText(
        "Update your payment method in billing management.",
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
      screen.getByText(new RegExp(`subscription will cancel on ${APR_1_2030}`)),
    ).toBeInTheDocument();
    expect(
      screen.getByText(new RegExp(`4 seats from ${APR_1_2030}`)),
    ).toBeInTheDocument();
    expect(screen.getByText(APR_1_2030)).toBeInTheDocument();
    expect(screen.queryByText(MAR_1_2030)).not.toBeInTheDocument();
    expect(
      screen.queryByText(new RegExp(`subscription will cancel on ${MAR_1_2030}`)),
    ).not.toBeInTheDocument();
    expect(screen.queryByText(/Pro remains available/)).not.toBeInTheDocument();
  });

  it("shows Cloud's explicit unlimited limits on a normal load", () => {
    Object.assign(mocks.entitlements, {
      plan: "pro",
      status: "active",
      limits: {
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
    expect(screen.getByText("Unlimited")).toBeInTheDocument();
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
        "Update your payment method in billing management.",
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

  it("checks out Business for the selected interval", async () => {
    const user = userEvent.setup();
    renderWithI18n(<BillingTab />);

    await user.click(screen.getByRole("button", { name: "Yearly" }));
    await user.click(
      screen.getByRole("button", { name: "Upgrade to Business" }),
    );
    expect(screen.getByText(/^Plan: Business\./)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Continue to Stripe" }));

    await waitFor(() => expect(mocks.checkout).toHaveBeenCalledOnce());
    expect(mocks.checkout.mock.calls[0]?.[0]).toMatchObject({
      plan: "business",
      interval: "year",
    });
  });

  it("starts a new Checkout intent when the plan changes", async () => {
    const user = userEvent.setup();
    mocks.checkout.mockRejectedValueOnce(new Error("network lost"));
    renderWithI18n(<BillingTab />);

    await user.click(screen.getByRole("button", { name: "Upgrade to Pro" }));
    await user.click(screen.getByRole("button", { name: "Continue to Stripe" }));
    await screen.findByText("Billing action failed");
    await user.click(
      screen.getByRole("button", { name: "Upgrade to Business" }),
    );
    await user.click(screen.getByRole("button", { name: "Continue to Stripe" }));

    await waitFor(() => expect(mocks.checkout).toHaveBeenCalledTimes(2));
    expect(mocks.checkout.mock.calls[1]?.[0].plan).toBe("business");
    expect(mocks.checkout.mock.calls[1]?.[0].idempotencyKey).not.toBe(
      mocks.checkout.mock.calls[0]?.[0].idempotencyKey,
    );
  });

  describe("plan switching", () => {
    function setPaidPlan(plan: "pro" | "business") {
      Object.assign(mocks.entitlements, {
        plan,
        status: "active",
        limits: { autopilotRuns: { mode: "unlimited", limit: null } },
        currentPeriodEnd: "2030-02-01T00:00:00Z",
      });
      setSeatCapacity({ humanMembers: 4, purchased: 4 });
      mocks.summary.billingInterval = "month";
      mocks.summary.availableActions.changePlan = true;
    }

    it("shows a confirming plan change and lets a manager check it now", async () => {
      const user = userEvent.setup();
      setPaidPlan("pro");
      mocks.summary.availableActions.changePlan = false;
      mocks.summary.availableActions.checkPlanChange = true;
      mocks.summary.planChange = {
        targetPlan: "business",
        state: "confirming",
        since: "2030-01-15T00:00:00Z",
      };
      mocks.checkPlanChange.mockResolvedValue({
        workspaceId: "workspace-1",
        action: "none",
        version: 8,
      });
      renderWithI18n(<BillingTab />);

      expect(screen.getByText("Plan change")).toBeInTheDocument();
      expect(
        screen.getByText(
          "Switching to Business. Waiting for Stripe to confirm; seats and the plan can't be changed until then.",
        ),
      ).toBeInTheDocument();
      expect(screen.queryByText("Change plan")).not.toBeInTheDocument();

      await user.click(screen.getByRole("button", { name: "Check status" }));

      await waitFor(() => expect(mocks.checkPlanChange).toHaveBeenCalledOnce());
      expect(mocks.refetchSummary).toHaveBeenCalled();
    });

    it("tells the user to contact support when a plan change is unresolved", () => {
      setPaidPlan("pro");
      mocks.summary.availableActions.changePlan = false;
      mocks.summary.planChange = {
        targetPlan: "business",
        state: "unresolved",
        since: "2030-01-15T00:00:00Z",
      };
      renderWithI18n(<BillingTab />);

      expect(
        screen.getByText(
          "The switch to Business could not be confirmed. Contact support to settle it; seats and the plan can't be changed until then.",
        ),
      ).toBeInTheDocument();
      expect(screen.getByText("Needs support")).toBeInTheDocument();
      expect(
        screen.queryByRole("button", { name: "Check status" }),
      ).not.toBeInTheDocument();
    });

    it("explains a plan change refused by an unresolved one", async () => {
      const user = userEvent.setup();
      setPaidPlan("pro");
      mocks.changePlan.mockRejectedValueOnce(
        new ApiError("request failed", 409, "Conflict", {
          code: "plan_change_unresolved",
        }),
      );
      renderWithI18n(<BillingTab />);

      await user.click(
        screen.getByRole("button", { name: "Upgrade to Business" }),
      );
      const dialog = await screen.findByRole("dialog");
      await within(dialog).findByText("$30.00");
      await user.click(
        within(dialog).getByRole("button", { name: "Upgrade to Business" }),
      );

      expect(
        await within(dialog).findByText(
          "The previous plan change could not be confirmed. Contact support before changing plans.",
        ),
      ).toBeInTheDocument();
      expect(mocks.refetchSummary).toHaveBeenCalled();
    });

    it("offers no switch unless Cloud allows it", () => {
      setPaidPlan("pro");
      mocks.summary.availableActions.changePlan = false;

      renderWithI18n(<BillingTab />);

      expect(screen.getByText("Pro")).toBeInTheDocument();
      expect(screen.queryByText("Change plan")).not.toBeInTheDocument();
      expect(
        screen.queryByRole("button", { name: "Upgrade to Business" }),
      ).not.toBeInTheDocument();
    });

    it("quotes and confirms an upgrade from Pro to Business", async () => {
      const user = userEvent.setup();
      setPaidPlan("pro");
      renderWithI18n(<BillingTab />);

      await user.click(
        screen.getByRole("button", { name: "Upgrade to Business" }),
      );
      const dialog = await screen.findByRole("dialog");
      expect(mocks.previewPlanChange).toHaveBeenCalledWith({
        plan: "business",
      });
      expect(await within(dialog).findByText("$30.00")).toBeInTheDocument();
      expect(within(dialog).getByText("Charged today")).toBeInTheDocument();
      expect(within(dialog).getByText("$120.00")).toBeInTheDocument();
      expect(within(dialog).getByText("4 seats")).toBeInTheDocument();

      await user.click(
        within(dialog).getByRole("button", { name: "Upgrade to Business" }),
      );

      await waitFor(() => expect(mocks.changePlan).toHaveBeenCalledOnce());
      const request = mocks.changePlan.mock.calls[0]?.[0];
      expect(request).toMatchObject({
        plan: "business",
        expectedSubscriptionVersion: 7,
        quotedAt: "2030-01-15T00:00:00Z",
        acceptedProrationAmount: 3000,
        currency: "usd",
      });
      expect(request.idempotencyKey).toMatch(
        /^workspace-plan-change-workspace-1-/,
      );
      expect(
        await screen.findByText("This workspace is now on Business."),
      ).toBeInTheDocument();
      expect(mocks.refetchSummary).toHaveBeenCalled();
    });

    it("shows a downgrade credit and warns that Business settings become read-only", async () => {
      const user = userEvent.setup();
      setPaidPlan("business");
      mocks.previewPlanChange.mockResolvedValue({
        currentPlan: "business",
        targetPlan: "pro",
        interval: "month",
        seats: 4,
        subscriptionVersion: 8,
        currency: "usd",
        prorationAmount: -3000,
        nextInvoiceAmount: 6000,
        invoiceNow: false,
        quotedAt: "2030-01-15T00:00:00Z",
      });
      mocks.changePlan.mockResolvedValue({
        plan: "pro",
        previousPlan: "business",
        interval: "month",
        seats: 4,
        currency: "usd",
        prorationAmount: -3000,
        invoiceNow: false,
      });
      renderWithI18n(<BillingTab />);

      await user.click(screen.getByRole("button", { name: "Switch to Pro" }));
      const dialog = await screen.findByRole("dialog");
      expect(
        within(dialog).getByText(/become read-only/),
      ).toBeInTheDocument();
      expect(
        await within(dialog).findByText("Credit on next invoice"),
      ).toBeInTheDocument();
      expect(within(dialog).getByText("$30.00")).toBeInTheDocument();

      await user.click(
        within(dialog).getByRole("button", { name: "Switch to Pro" }),
      );

      await waitFor(() => expect(mocks.changePlan).toHaveBeenCalledOnce());
      expect(mocks.changePlan.mock.calls[0]?.[0]).toMatchObject({
        plan: "pro",
        acceptedProrationAmount: -3000,
      });
      expect(
        await screen.findByText("This workspace is now on Pro."),
      ).toBeInTheDocument();
    });

    it("tells a downgrading manager this month's limit when add-ons were bought", async () => {
      const user = userEvent.setup();
      setPaidPlan("business");
      mocks.entitlements.limits = {
        autopilotRuns: { mode: "limited", limit: 4000 },
      };
      Object.assign(mocks.summary, {
        autopilotAddon: {
          purchasedRuns: 1000,
          periodStart: "2030-01-01T00:00:00Z",
          periodEnd: "2030-02-01T00:00:00Z",
        },
      });
      mocks.previewPlanChange.mockResolvedValue({
        currentPlan: "business",
        targetPlan: "pro",
        interval: "month",
        seats: 1,
        subscriptionVersion: 8,
        currency: "usd",
        prorationAmount: -1200,
        nextInvoiceAmount: 1500,
        invoiceNow: false,
        quotedAt: "2030-01-15T00:00:00Z",
        autopilotRunsLimitAfter: 2000,
      });
      renderWithI18n(<BillingTab />);

      await user.click(screen.getByRole("button", { name: "Switch to Pro" }));
      const dialog = await screen.findByRole("dialog");
      expect(
        await within(dialog).findByText(
          "This month's limit becomes 2,000 runs after the switch, including runs you bought. 5 runs are used this month.",
        ),
      ).toBeInTheDocument();
    });

    it("requests a fresh quote when Cloud reports the quote changed", async () => {
      const user = userEvent.setup();
      setPaidPlan("pro");
      mocks.changePlan.mockRejectedValueOnce(
        new ApiError("request failed", 409, "Conflict", {
          code: "plan_change_quote_changed",
        }),
      );
      renderWithI18n(<BillingTab />);

      await user.click(
        screen.getByRole("button", { name: "Upgrade to Business" }),
      );
      const dialog = await screen.findByRole("dialog");
      await within(dialog).findByText("$30.00");
      await user.click(
        within(dialog).getByRole("button", { name: "Upgrade to Business" }),
      );

      expect(
        await within(dialog).findByText(
          "The subscription or price changed. Review the refreshed quote before confirming again.",
        ),
      ).toBeInTheDocument();
      await waitFor(() =>
        expect(mocks.previewPlanChange).toHaveBeenCalledTimes(2),
      );
      expect(mocks.refetchSummary).toHaveBeenCalled();
    });

    it("explains that another plan change holds the subscription", async () => {
      const user = userEvent.setup();
      setPaidPlan("pro");
      mocks.changePlan.mockRejectedValueOnce(
        new ApiError("request failed", 409, "Conflict", {
          code: "plan_change_in_progress",
        }),
      );
      renderWithI18n(<BillingTab />);

      await user.click(
        screen.getByRole("button", { name: "Upgrade to Business" }),
      );
      const dialog = await screen.findByRole("dialog");
      await within(dialog).findByText("$30.00");
      await user.click(
        within(dialog).getByRole("button", { name: "Upgrade to Business" }),
      );

      expect(
        await within(dialog).findByText(
          "A plan change is still being processed. Try again in a few minutes.",
        ),
      ).toBeInTheDocument();
      expect(mocks.refetchSummary).toHaveBeenCalled();
      // Not treated as a changed quote: no new preview is requested.
      expect(mocks.previewPlanChange).toHaveBeenCalledTimes(1);
    });

    it("recovers after a lost confirmation once the dialog is reopened", async () => {
      const user = userEvent.setup();
      setPaidPlan("pro");
      mocks.changePlan.mockRejectedValueOnce(
        new ApiError("request failed", 503, "Service Unavailable", {}),
      );
      renderWithI18n(<BillingTab />);

      await user.click(
        screen.getByRole("button", { name: "Upgrade to Business" }),
      );
      let dialog = await screen.findByRole("dialog");
      await within(dialog).findByText("$30.00");
      await user.click(
        within(dialog).getByRole("button", { name: "Upgrade to Business" }),
      );
      await waitFor(() => expect(mocks.changePlan).toHaveBeenCalledOnce());
      const lostKey = mocks.changePlan.mock.calls[0]?.[0].idempotencyKey;
      // Closing the dialog drops the retry key, as a reload would.
      await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
      await waitFor(() =>
        expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
      );

      // Right after the timeout Cloud still holds the change.
      mocks.previewPlanChange.mockRejectedValueOnce(
        new ApiError("request failed", 409, "Conflict", {
          code: "plan_change_in_progress",
        }),
      );
      await user.click(
        screen.getByRole("button", { name: "Upgrade to Business" }),
      );
      dialog = await screen.findByRole("dialog");
      expect(
        await within(dialog).findByText(
          "A plan change is still being processed. Try again in a few minutes.",
        ),
      ).toBeInTheDocument();
      await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
      await waitFor(() =>
        expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
      );

      // Once Cloud has settled it, a fresh quote goes through under a new key.
      await user.click(
        screen.getByRole("button", { name: "Upgrade to Business" }),
      );
      dialog = await screen.findByRole("dialog");
      await within(dialog).findByText("$30.00");
      await user.click(
        within(dialog).getByRole("button", { name: "Upgrade to Business" }),
      );
      await waitFor(() => expect(mocks.changePlan).toHaveBeenCalledTimes(2));
      expect(mocks.changePlan.mock.calls[1]?.[0].idempotencyKey).not.toBe(
        lostKey,
      );
      expect(
        await screen.findByText("This workspace is now on Business."),
      ).toBeInTheDocument();
    });

    it("explains a quote refused while another plan change is in flight", async () => {
      const user = userEvent.setup();
      setPaidPlan("pro");
      mocks.previewPlanChange.mockRejectedValueOnce(
        new ApiError("request failed", 409, "Conflict", {
          code: "plan_change_in_progress",
        }),
      );
      renderWithI18n(<BillingTab />);

      await user.click(
        screen.getByRole("button", { name: "Upgrade to Business" }),
      );
      const dialog = await screen.findByRole("dialog");

      expect(
        await within(dialog).findByText(
          "A plan change is still being processed. Try again in a few minutes.",
        ),
      ).toBeInTheDocument();
    });

    it("keeps the subscription unchanged and explains a declined upgrade payment", async () => {
      const user = userEvent.setup();
      setPaidPlan("pro");
      mocks.changePlan.mockRejectedValueOnce(
        new ApiError("request failed", 402, "Payment Required", {
          code: "plan_change_payment_failed",
        }),
      );
      renderWithI18n(<BillingTab />);

      await user.click(
        screen.getByRole("button", { name: "Upgrade to Business" }),
      );
      const dialog = await screen.findByRole("dialog");
      await within(dialog).findByText("$30.00");
      await user.click(
        within(dialog).getByRole("button", { name: "Upgrade to Business" }),
      );

      expect(
        await within(dialog).findByText(
          "Payment could not be completed. Update the payment method in billing management, then try again.",
        ),
      ).toBeInTheDocument();
      expect(
        within(dialog).getByRole("button", { name: "Upgrade to Business" }),
      ).toBeDisabled();
    });

    it("reuses the idempotency key when retrying the same quote", async () => {
      const user = userEvent.setup();
      setPaidPlan("pro");
      mocks.changePlan.mockRejectedValueOnce(new Error("network lost"));
      renderWithI18n(<BillingTab />);

      await user.click(
        screen.getByRole("button", { name: "Upgrade to Business" }),
      );
      const dialog = await screen.findByRole("dialog");
      await within(dialog).findByText("$30.00");
      const confirm = within(dialog).getByRole("button", {
        name: "Upgrade to Business",
      });
      await user.click(confirm);
      await within(dialog).findByText(
        "The plan could not be changed. Retry with the same quote.",
      );
      await user.click(confirm);

      await waitFor(() => expect(mocks.changePlan).toHaveBeenCalledTimes(2));
      expect(mocks.changePlan.mock.calls[1]?.[0].idempotencyKey).toBe(
        mocks.changePlan.mock.calls[0]?.[0].idempotencyKey,
      );
    });

    it("closes the dialog and refreshes when the plan can no longer change", async () => {
      const user = userEvent.setup();
      setPaidPlan("pro");
      mocks.previewPlanChange.mockRejectedValueOnce(
        new ApiError("request failed", 409, "Conflict", {
          code: "subscription_not_active",
        }),
      );
      renderWithI18n(<BillingTab />);

      await user.click(
        screen.getByRole("button", { name: "Upgrade to Business" }),
      );

      expect(
        await screen.findByText(
          "This subscription can't change plans right now. The current plan has been refreshed.",
        ),
      ).toBeInTheDocument();
      await waitFor(() =>
        expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
      );
      expect(mocks.refetchSummary).toHaveBeenCalled();
    });
  });
  describe("no-card trial", () => {
    const DAY = 24 * 60 * 60 * 1000;

    function setTrial(state: "active" | "expired", startedAt: number, days = 14) {
      const endsAt = startedAt + days * DAY;
      mocks.summary.trial = {
        plan: "business",
        state,
        startedAt: new Date(startedAt).toISOString(),
        endsAt: new Date(endsAt).toISOString(),
      };
      if (state === "active") {
        Object.assign(mocks.entitlements, {
          plan: "business",
          status: "trialing",
          limits: { autopilotRuns: { mode: "unlimited", limit: null } },
          currentPeriodEnd: new Date(endsAt).toISOString(),
        });
      }
      return endsAt;
    }

    it("shows where the trial stands and what each Business feature keeps", () => {
      const endsAt = setTrial("active", Date.now() - 4.5 * DAY);
      renderWithI18n(<BillingTab />);

      expect(screen.getByRole("heading", { name: "Business trial" })).toBeInTheDocument();
      expect(screen.getByText("Trial · 10 days left")).toBeInTheDocument();
      expect(
        screen.getByText(
          `Trial until ${new Intl.DateTimeFormat("en", { dateStyle: "medium" }).format(new Date(endsAt))}. No payment method is on file, so the workspace moves to Free afterwards and is not charged.`,
        ),
      ).toBeInTheDocument();
      expect(screen.getAllByText("Day 5 of 14").length).toBeGreaterThan(0);
      expect(screen.getByText("Private projects")).toBeInTheDocument();
      expect(
        screen.getByText("Stay private after the trial. Their access lists become read-only."),
      ).toBeInTheDocument();
      // A trialling manager subscribes through the same plan cards.
      expect(screen.getByRole("button", { name: "Upgrade to Business" })).toBeInTheDocument();
    });

    it("takes a manager from the trial card to the plan cards", async () => {
      setTrial("active", Date.now() - 4.5 * DAY);
      const scrollIntoView = vi.fn();
      const original = Element.prototype.scrollIntoView;
      Element.prototype.scrollIntoView = scrollIntoView;
      try {
        renderWithI18n(<BillingTab />);
        await userEvent.setup().click(screen.getByRole("button", { name: "Choose a plan" }));

        expect(scrollIntoView).toHaveBeenCalledOnce();
        expect(scrollIntoView.mock.contexts[0]).toHaveAttribute("data-settings-anchor", "billing-upgrade");
      } finally {
        Element.prototype.scrollIntoView = original;
      }
    });

    it("offers no plan choice to a member who cannot subscribe", () => {
      setTrial("active", Date.now() - 4.5 * DAY);
      mocks.summary.availableActions.checkout = false;
      renderWithI18n(<BillingTab />);

      expect(screen.queryByRole("button", { name: "Choose a plan" })).not.toBeInTheDocument();
    });

    it("points an ended trial at the plans below", () => {
      const endsAt = setTrial("expired", Date.now() - 20 * DAY);
      renderWithI18n(<BillingTab />);

      expect(screen.getByText("Business trial ended")).toBeInTheDocument();
      expect(
        screen.getByText(
          `The trial ended on ${new Intl.DateTimeFormat("en", { dateStyle: "medium" }).format(new Date(endsAt))}. Choose a plan below to get Business features back.`,
        ),
      ).toBeInTheDocument();
      expect(screen.queryByText(/days? left/)).not.toBeInTheDocument();
    });

    it("does not send a member to plan cards that are hidden after trial expiry", () => {
      setTrial("expired", Date.now() - 15 * DAY);
      mocks.summary.availableActions.checkout = false;
      renderWithI18n(<BillingTab />);
      expect(screen.getByText("Business trial ended")).toBeInTheDocument();
      expect(screen.queryByRole("button", { name: "Upgrade to Pro" })).not.toBeInTheDocument();
      expect(screen.queryByText(/Choose a plan below/)).not.toBeInTheDocument();
      expect(screen.getByText(/Ask a workspace admin to upgrade/)).toBeInTheDocument();
    });

    it("says nothing about an end that only this clock has seen", () => {
      // Cloud still reports the trial active, a moment past its end here.
      setTrial("active", Date.now() - 14 * DAY - 1000);
      renderWithI18n(<BillingTab />);
      expect(screen.queryByText("Business trial ended")).not.toBeInTheDocument();
    });
  });
});
