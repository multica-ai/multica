// @vitest-environment node

import { describe, expect, it } from "vitest";
import type {
  AutopilotQuotaUsage,
  WorkspaceSubscriptionEntitlements,
  WorkspaceSubscriptionSummary,
} from "@multica/core/types";
import {
  canPurchaseWorkspaceSubscription,
  hasManagedWorkspaceSubscription,
  resolveAutopilotUsage,
} from "./billing-state";

const freeEntitlements: WorkspaceSubscriptionEntitlements = {
  workspaceId: "workspace-1",
  plan: "free",
  status: "inactive",
  seats: 3,
  issueWindow: 17,
  autopilotRuns: 7,
  currentPeriodEnd: null,
  snapshotExpiresAt: null,
  version: 1,
};

const quotaUsage: AutopilotQuotaUsage = {
  action: "enforce",
  used: 3,
  reserved: 2,
  limit: 7,
  period_start: "2030-01-01T00:00:00Z",
  period_end: "2030-02-01T00:00:00Z",
  reset_at: "2030-02-01T00:00:00Z",
  blocked_counts: {},
};

describe("resolveAutopilotUsage", () => {
  it("counts reserved runs toward progress and the reached decision", () => {
    expect(
      resolveAutopilotUsage(freeEntitlements, quotaUsage, false, false),
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
        { ...quotaUsage, used: 5 },
        false,
        false,
      ),
    ).toMatchObject({ total: 7, reached: true, progress: 100 });
  });

  it("shows Pro as unlimited from entitlement even when usage is unavailable", () => {
    expect(
      resolveAutopilotUsage(
        { ...freeEntitlements, plan: "pro", autopilotRuns: null },
        undefined,
        true,
        true,
      ),
    ).toEqual({ kind: "unlimited" });
  });

  it("does not turn missing or disabled limited usage into zero or unlimited", () => {
    expect(
      resolveAutopilotUsage(freeEntitlements, undefined, true, false),
    ).toEqual({ kind: "unavailable" });
    expect(
      resolveAutopilotUsage(
        freeEntitlements,
        {
          ...quotaUsage,
          action: "off",
          used: null,
          reserved: null,
          limit: null,
          reset_at: null,
        },
        false,
        false,
      ),
    ).toEqual({ kind: "unavailable" });
  });

  it("keeps authoritative metered usage independent of entitlement unlimited", () => {
    expect(
      resolveAutopilotUsage(
        { ...freeEntitlements, plan: "pro", autopilotRuns: null },
        quotaUsage,
        false,
        false,
      ),
    ).toMatchObject({ kind: "metered", total: 5, limit: 7 });
  });

  it("does not derive unlimited when the entitlement fact is not trusted", () => {
    expect(
      resolveAutopilotUsage(
        { ...freeEntitlements, plan: "pro", autopilotRuns: null },
        undefined,
        true,
        false,
      ),
    ).toEqual({ kind: "unavailable" });
  });
});

describe("billing subscription state", () => {
  it("prefers subscription facts and keeps safe compatibility fallbacks", () => {
    const summary = {
      entitlement: freeEntitlements,
      billingInterval: null,
      actualSeats: 3,
      billedSeats: null,
      pendingSeatQuantity: null,
      cancelAtPeriodEnd: false,
      graceUntil: null,
      hasStripeCustomer: true,
    } satisfies WorkspaceSubscriptionSummary;

    expect(hasManagedWorkspaceSubscription(freeEntitlements, summary)).toBe(
      true,
    );
    expect(
      hasManagedWorkspaceSubscription(
        { ...freeEntitlements, status: "incomplete_expired" },
        undefined,
      ),
    ).toBe(true);
    expect(hasManagedWorkspaceSubscription(freeEntitlements, undefined)).toBe(
      false,
    );
  });

  it.each([
    ["inactive", true],
    ["canceled", true],
    ["incomplete_expired", true],
    ["active", false],
    ["trialing", false],
    ["past_due", false],
    ["incomplete", false],
    ["paused", false],
    ["unpaid", false],
    ["future_status", false],
  ])("allows a Free workspace in %s to purchase: %s", (status, expected) => {
    expect(
      canPurchaseWorkspaceSubscription({
        ...freeEntitlements,
        status,
      }),
    ).toBe(expected);
  });

  it("never offers Checkout while Pro is currently enforced", () => {
    expect(
      canPurchaseWorkspaceSubscription({
        ...freeEntitlements,
        plan: "pro",
        status: "active",
      }),
    ).toBe(false);
  });
});
