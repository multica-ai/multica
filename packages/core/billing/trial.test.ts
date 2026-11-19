// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { WorkspaceSubscriptionSummary, WorkspaceTrial } from "../types";
import {
  DAY_MS,
  TRIAL_CONFIRM_SETTLE_MS,
  TRIAL_END_SETTLE_MS,
  resolveWorkspaceTrial,
} from "./trial";

const startedAt = Date.parse("2026-12-05T08:00:00Z");
const endsAt = startedAt + 14 * DAY_MS;

function summary(
  trial: Partial<WorkspaceTrial> | null,
  plan = trial?.state === "active" ? "business" : "free",
): WorkspaceSubscriptionSummary {
  return {
    entitlement: {
      workspaceId: "ws-1",
      plan,
      status: plan === "business" ? "trialing" : "inactive",
      seats: 7,
      limits: { autopilotRuns: { mode: "unlimited", limit: null } },
      currentPeriodEnd: null,
      snapshotExpiresAt: null,
      version: 0,
    },
    billingInterval: null,
    humanMembers: 7,
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
    },
    planChange: null,
    trial: trial
      ? {
          plan: "business",
          state: "active",
          startedAt: new Date(startedAt).toISOString(),
          endsAt: new Date(endsAt).toISOString(),
          ...trial,
        }
      : null,
  };
}

describe("resolveWorkspaceTrial", () => {
  it("has nothing to show without a trial or after converting", () => {
    expect(resolveWorkspaceTrial(null, startedAt)).toEqual({ kind: "none" });
    expect(resolveWorkspaceTrial(summary(null), startedAt)).toEqual({ kind: "none" });
    expect(
      resolveWorkspaceTrial(summary({ state: "converted" }, "pro"), startedAt),
    ).toEqual({ kind: "none" });
  });

  it.each([
    // [elapsed, daysLeft, day, warning, reminder]
    [0, 14, 1, false, null],
    [6.5 * DAY_MS, 8, 7, false, null],
    [7 * DAY_MS, 7, 8, false, 7],
    [10.5 * DAY_MS, 4, 11, false, 7],
    [11 * DAY_MS, 3, 12, true, 3],
    [13 * DAY_MS + 1, 1, 14, true, 1],
    [14 * DAY_MS - 1, 1, 14, true, 1],
  ] as const)(
    "counts whole days left from the end (%d ms in)",
    (elapsed, daysLeft, day, warning, reminder) => {
      const view = resolveWorkspaceTrial(summary({}), startedAt + elapsed);
      expect(view).toMatchObject({
        kind: "active",
        daysLeft,
        day,
        totalDays: 14,
        warning,
        reminder,
      });
    },
  );

  it("reads a stale active summary as ended once the end has passed", () => {
    expect(resolveWorkspaceTrial(summary({}), endsAt)).toEqual({
      kind: "ended",
      plan: "business",
      endsAt,
      recent: true,
      // Only this clock says so; Cloud has not confirmed the downgrade.
      confirmed: false,
      settledAt: null,
      settled: false,
    });
    expect(
      resolveWorkspaceTrial(summary({ state: "expired" }), endsAt),
    ).toMatchObject({ kind: "ended", confirmed: true });
  });

  it("offers the upgrade for a week after the end, then only on Billing", () => {
    const expired = summary({ state: "expired" });
    expect(resolveWorkspaceTrial(expired, endsAt + 7 * DAY_MS - 1)).toMatchObject({
      kind: "ended",
      recent: true,
    });
    expect(resolveWorkspaceTrial(expired, endsAt + 7 * DAY_MS)).toMatchObject({
      kind: "ended",
      recent: false,
    });
  });

  it("settles after Cloud's confirmation, never before the end's own settle point", () => {
    const expired = summary({ state: "expired" });
    // Confirmed late (this clock behind Cloud's): settles shortly after.
    const late = endsAt + 10 * 60 * 1000;
    expect(resolveWorkspaceTrial(expired, late, late)).toMatchObject({
      settledAt: late + TRIAL_CONFIRM_SETTLE_MS,
      settled: false,
    });
    expect(
      resolveWorkspaceTrial(expired, late + TRIAL_CONFIRM_SETTLE_MS, late),
    ).toMatchObject({ settled: true });
    // Confirmed right at the end: the end's settle point still applies.
    expect(resolveWorkspaceTrial(expired, endsAt, endsAt)).toMatchObject({
      settledAt: endsAt + TRIAL_END_SETTLE_MS,
    });
  });

  it("settles once the server can no longer hold the trial's policy", () => {
    const expired = summary({ state: "expired" });
    expect(
      resolveWorkspaceTrial(expired, endsAt + TRIAL_END_SETTLE_MS - 1),
    ).toMatchObject({ kind: "ended", settled: false });
    expect(
      resolveWorkspaceTrial(expired, endsAt + TRIAL_END_SETTLE_MS),
    ).toMatchObject({ kind: "ended", settled: true });
  });

  it("skips reminders a short trial never reaches", () => {
    const short = summary({
      endsAt: new Date(startedAt + 5 * DAY_MS).toISOString(),
    });
    expect(resolveWorkspaceTrial(short, startedAt)).toMatchObject({
      kind: "active",
      daysLeft: 5,
      reminder: null,
    });
  });

  it("ignores timestamps it cannot use", () => {
    expect(
      resolveWorkspaceTrial(summary({ endsAt: "not a date" }), startedAt),
    ).toEqual({ kind: "none" });
    expect(
      resolveWorkspaceTrial(
        summary({ endsAt: new Date(startedAt).toISOString() }),
        startedAt,
      ),
    ).toEqual({ kind: "none" });
  });
});
