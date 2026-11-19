// @vitest-environment node

import { describe, expect, it } from "vitest";
import type {
  AutopilotQuotaUsage,
  WorkspaceSubscriptionEntitlements,
  WorkspaceSubscriptionSummary,
} from "@multica/core/types";
import {
  hasActiveWorkspaceSeatCapacity,
  resolveAutopilotUsage,
  resolvePlanChangeTarget,
  yearlyPriceMonthlyEquivalent,
} from "./billing-state";

const freeEntitlements: WorkspaceSubscriptionEntitlements = {
  workspaceId: "workspace-1",
  plan: "free",
  status: "inactive",
  seats: 3,
  limits: {
    autopilotRuns: { mode: "limited", limit: 7 },
  },
  currentPeriodEnd: null,
  snapshotExpiresAt: null,
  version: 1,
};

const quotaUsage: AutopilotQuotaUsage = {
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
};

describe("resolveAutopilotUsage", () => {
  it("counts reserved runs toward progress and the reached decision", () => {
    expect(
      resolveAutopilotUsage(freeEntitlements, quotaUsage, false),
    ).toEqual({
      kind: "metered",
      used: 3,
      reserved: 2,
      total: 5,
      limit: 7,
      progress: 500 / 7,
      reached: false,
      resetAt: "2030-02-01T00:00:00Z",
    });

    expect(
      resolveAutopilotUsage(
        freeEntitlements,
        { ...quotaUsage, used: 5, total: 7, reached: true },
        false,
      ),
    ).toMatchObject({ total: 7, reached: true, progress: 100 });
  });

  it("renders the server's explicit unlimited mode", () => {
    expect(
      resolveAutopilotUsage(
        {
          ...freeEntitlements,
          limits: {
            ...freeEntitlements.limits,
            autopilotRuns: { mode: "unlimited", limit: null },
          },
        },
        undefined,
        true,
      ),
    ).toEqual({ kind: "unlimited" });
  });

  it("does not turn missing or disabled limited usage into zero or unlimited", () => {
    expect(
      resolveAutopilotUsage(freeEntitlements, undefined, true),
    ).toEqual({ kind: "unavailable" });
    expect(
      resolveAutopilotUsage(
        freeEntitlements,
        {
          ...quotaUsage,
          action: "off",
          used: null,
          reserved: null,
          total: null,
          limit: null,
          reached: null,
          reset_at: null,
        },
        false,
      ),
    ).toEqual({ kind: "unavailable" });
    expect(
      resolveAutopilotUsage(
        freeEntitlements,
        {
          ...quotaUsage,
          action: "observe",
          total: quotaUsage.limit,
          reached: null,
        },
        false,
      ),
    ).toEqual({ kind: "unavailable" });
  });

  it("uses the explicit limited mode even when plan and status say Pro", () => {
    expect(
      resolveAutopilotUsage(
        {
          ...freeEntitlements,
          plan: "pro",
          status: "active",
        },
        quotaUsage,
        false,
      ),
    ).toMatchObject({ kind: "metered", limit: 7 });
  });
});

describe("billing subscription state", () => {
  it("keeps billing history separate from current seat capacity", () => {
    const canceledSummary = {
      entitlement: freeEntitlements,
      billingInterval: null,
      humanMembers: 3,
      seatCapacity: null,
      cancelAtPeriodEnd: false,
      graceUntil: null,
      hasStripeCustomer: true,
      availableActions: {
        checkout: true,
        portal: true,
        purchaseSeats: false,
        changePlan: false,
        checkPlanChange: false,
      },
      planChange: null,
      trial: null,
    } satisfies WorkspaceSubscriptionSummary;

    expect(hasActiveWorkspaceSeatCapacity(canceledSummary)).toBe(false);

    const activeSummary = {
      ...canceledSummary,
      seatCapacity: {
        purchased: 5,
        used: 3,
        reserved: 1,
        available: 1,
        overcommitted: false,
        version: 2,
        pendingQuantity: null,
        activePurchase: null,
      },
    } satisfies WorkspaceSubscriptionSummary;
    expect(hasActiveWorkspaceSeatCapacity(activeSummary)).toBe(true);
  });
});

describe("resolvePlanChangeTarget", () => {
  const summary = (
    plan: string,
    changePlan: boolean,
  ): WorkspaceSubscriptionSummary => ({
    entitlement: { ...freeEntitlements, plan, status: "active" },
    billingInterval: "month",
    humanMembers: 3,
    seatCapacity: null,
    cancelAtPeriodEnd: false,
    graceUntil: null,
    hasStripeCustomer: true,
    availableActions: {
      checkout: false,
      portal: true,
      purchaseSeats: true,
      changePlan,
      checkPlanChange: false,
    },
    planChange: null,
    trial: null,
  });

  it("switches between Pro and Business only when Cloud offers it", () => {
    expect(resolvePlanChangeTarget(summary("pro", true))).toBe("business");
    expect(resolvePlanChangeTarget(summary("business", true))).toBe("pro");
    expect(resolvePlanChangeTarget(summary("pro", false))).toBeNull();
  });

  it("offers no switch for Free, an unknown plan, or a missing summary", () => {
    expect(resolvePlanChangeTarget(summary("free", true))).toBeNull();
    expect(resolvePlanChangeTarget(summary("enterprise", true))).toBeNull();
    expect(resolvePlanChangeTarget(null)).toBeNull();
  });
});

describe("yearlyPriceMonthlyEquivalent", () => {
  it("divides a yearly price that splits evenly into months", () => {
    expect(
      yearlyPriceMonthlyEquivalent({
        currency: "usd",
        unitAmount: 14400,
        interval: "year",
        intervalCount: 1,
      }),
    ).toBe(1200);
  });

  it("returns null instead of rounding or converting a monthly price", () => {
    expect(
      yearlyPriceMonthlyEquivalent({
        currency: "usd",
        unitAmount: 10000,
        interval: "year",
        intervalCount: 1,
      }),
    ).toBeNull();
    expect(
      yearlyPriceMonthlyEquivalent({
        currency: "usd",
        unitAmount: 1500,
        interval: "month",
        intervalCount: 1,
      }),
    ).toBeNull();
  });
});
