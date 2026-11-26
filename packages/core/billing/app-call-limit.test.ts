// @vitest-environment node
import { describe, expect, it } from "vitest";

import { AppCallUsageSchema, WorkspaceSubscriptionEntitlementsSchema } from "../api/schemas";
import type { AppCallUsage } from "../types";
import { resolveAppCallRow } from "./app-call-limit";

const OFF: AppCallUsage = { action: "off", used: null, limit: null, reached: null, resetAt: null, members: [] };
const RESET_AT = "2026-12-01T00:00:00Z";

describe("AppCallUsageSchema", () => {
  it("reads an enforced limit with its figures and per-person breakdown", () => {
    expect(
      AppCallUsageSchema.parse({
        action: "enforce",
        used: 100,
        limit: 100,
        reached: true,
        period_start: "2026-11-01T00:00:00Z",
        reset_at: RESET_AT,
        members: [
          { user_id: "u1", calls: 70 },
          { user_id: null, calls: 30 },
        ],
      }),
    ).toEqual({
      action: "enforce",
      used: 100,
      limit: 100,
      reached: true,
      resetAt: RESET_AT,
      members: [
        { userId: "u1", calls: 70 },
        { userId: null, calls: 30 },
      ],
    });
  });

  it("never reports an observed limit as reached, and reads a missing breakdown as none", () => {
    expect(AppCallUsageSchema.parse({ action: "observe", used: 500, limit: 100, reset_at: RESET_AT })).toEqual({
      action: "observe",
      used: 500,
      limit: 100,
      reached: null,
      resetAt: RESET_AT,
      members: [],
    });
  });

  it("drops only a malformed breakdown", () => {
    expect(
      AppCallUsageSchema.parse({ action: "enforce", used: 1, limit: 5, reached: false, members: [{ calls: "x" }] }),
    ).toMatchObject({ used: 1, limit: 5, members: [] });
  });

  it("reads off, an unknown action, or missing figures as no limit", () => {
    expect(AppCallUsageSchema.parse({ action: "off" })).toEqual(OFF);
    expect(AppCallUsageSchema.parse({ action: "block", used: 1, limit: 5 })).toEqual(OFF);
    expect(AppCallUsageSchema.parse({ action: "enforce", used: "1", limit: 5 })).toEqual(OFF);
  });
});

describe("entitlement app_tool_calls", () => {
  const entitlement = (limits: Record<string, unknown>) => ({
    workspace_id: "ws",
    plan: "pro",
    status: "active",
    seats: 3,
    limits: { autopilot_runs: { mode: "limited", limit: 3000 }, ...limits },
    version: 1,
  });

  it("reads the plan's app call limit, and an older or malformed one as none", () => {
    expect(
      WorkspaceSubscriptionEntitlementsSchema.parse(entitlement({ app_tool_calls: { mode: "limited", limit: 1500 } }))
        .limits.appToolCalls,
    ).toEqual({ mode: "limited", limit: 1500 });
    expect(WorkspaceSubscriptionEntitlementsSchema.parse(entitlement({})).limits.appToolCalls).toBeNull();
    expect(
      WorkspaceSubscriptionEntitlementsSchema.parse(entitlement({ app_tool_calls: { mode: "bogus" } })).limits
        .appToolCalls,
    ).toBeNull();
  });
});

describe("resolveAppCallRow", () => {
  const usage = (over: Partial<AppCallUsage>): AppCallUsage => ({
    action: "enforce",
    used: 90,
    limit: 100,
    reached: false,
    resetAt: RESET_AT,
    members: [],
    ...over,
  });
  const plan = (limit: number) => ({ mode: "limited", limit });

  it("meters usage against the limit, and only an enforced limit is reached", () => {
    expect(resolveAppCallRow(usage({}), false, null)).toEqual({
      kind: "metered",
      used: 90,
      limit: 100,
      reached: false,
      resetAt: RESET_AT,
      members: [],
    });
    expect(resolveAppCallRow(usage({ used: 100 }), false, null)).toMatchObject({ reached: true });
    expect(resolveAppCallRow(usage({ action: "observe", used: 500 }), false, null)).toMatchObject({ reached: false });
  });

  // Right after an upgrade or a seat purchase the usage read can still carry
  // the policy cached before it; the entitlement has the new limit.
  it("takes the limit from the plan when the entitlement has one", () => {
    expect(resolveAppCallRow(usage({ used: 100 }), false, plan(1500))).toMatchObject({
      used: 100,
      limit: 1500,
      reached: false,
    });
  });

  it("shows a failed read as unavailable, and nothing without a limit", () => {
    expect(resolveAppCallRow(undefined, true, plan(100))).toEqual({ kind: "unavailable", limit: 100 });
    expect(resolveAppCallRow(undefined, true, null)).toBeNull();
    expect(resolveAppCallRow(OFF, false, plan(100))).toBeNull();
    expect(resolveAppCallRow(undefined, false, plan(100))).toBeNull();
  });
});
