import { describe, expect, it } from "vitest";
import {
  agentToolActionListOptions,
  agentToolApprovalListOptions,
  agentToolPolicyOptions,
  operationalCapabilityListOptions,
  operationalControlKeys,
  operationalSummaryOptions,
} from "./queries";

describe("operational control query contracts", () => {
  it("scopes every protected key to its workspace", () => {
    const actions = { event_type: "failed" as const, limit: 25 };
    const approvals = { status: "pending" as const, limit: 50 };

    expect(operationalControlKeys.policy("ws-a", "agent-1")).toEqual([
      "workspaces",
      "ws-a",
      "operational-controls",
      "agents",
      "agent-1",
      "policy",
    ]);
    expect(operationalControlKeys.actions("ws-a", "agent-1", actions)).not.toEqual(
      operationalControlKeys.actions("ws-b", "agent-1", actions),
    );
    expect(operationalControlKeys.approvals("ws-a", approvals)).not.toEqual(
      operationalControlKeys.approvals("ws-b", approvals),
    );
    expect(operationalControlKeys.capabilities("ws-a")).not.toEqual(
      operationalControlKeys.capabilities("ws-b"),
    );
    expect(
      operationalControlKeys.summary("ws-a", { days: 1, tz: "UTC" }),
    ).not.toEqual(
      operationalControlKeys.summary("ws-b", { days: 1, tz: "UTC" }),
    );
  });

  it("includes filters in list and window cache identities", () => {
    expect(
      operationalControlKeys.actions("ws-a", "agent-1", {
        event_type: "failed",
      }),
    ).not.toEqual(
      operationalControlKeys.actions("ws-a", "agent-1", {
        event_type: "succeeded",
      }),
    );
    expect(
      operationalControlKeys.approvals("ws-a", { status: "pending" }),
    ).not.toEqual(
      operationalControlKeys.approvals("ws-a", { status: "denied" }),
    );
    expect(
      operationalControlKeys.summary("ws-a", { days: 1, tz: "UTC" }),
    ).not.toEqual(
      operationalControlKeys.summary("ws-a", { days: 7, tz: "UTC" }),
    );
  });

  it("disables protected requests until their complete scope is available", () => {
    expect(agentToolPolicyOptions("", "agent-1").enabled).toBe(false);
    expect(agentToolPolicyOptions("ws-a", "").enabled).toBe(false);
    expect(agentToolActionListOptions("", "agent-1").enabled).toBe(false);
    expect(agentToolApprovalListOptions("").enabled).toBe(false);
    expect(operationalCapabilityListOptions("").enabled).toBe(false);
    expect(
      operationalSummaryOptions("", { days: 1, tz: "UTC" }).enabled,
    ).toBe(false);
  });

  it("keeps protected operational data short-lived and non-retrying", () => {
    for (const options of [
      agentToolPolicyOptions("ws-a", "agent-1"),
      agentToolActionListOptions("ws-a", "agent-1"),
      agentToolApprovalListOptions("ws-a"),
      operationalCapabilityListOptions("ws-a"),
      operationalSummaryOptions("ws-a", { days: 1, tz: "UTC" }),
    ]) {
      expect(options.staleTime).toBe(0);
      expect(options.retry).toBe(false);
    }
  });
});
