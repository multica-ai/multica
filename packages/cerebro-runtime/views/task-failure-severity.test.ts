import { describe, expect, it } from "vitest";
import { isInterruptionReason } from "./task-failure-severity";

describe("isInterruptionReason", () => {
  it("treats runtime and provider interruptions as interruptions", () => {
    expect(isInterruptionReason("runtime_recovery")).toBe(true);
    expect(isInterruptionReason("runtime_offline")).toBe(true);
    expect(isInterruptionReason("agent_error.provider_capacity_or_rate_limit")).toBe(true);
    expect(isInterruptionReason("agent_error.provider_server_error")).toBe(true);
    expect(isInterruptionReason("agent_error.provider_network")).toBe(true);
  });

  it("treats genuine failures as non-interruptions", () => {
    expect(isInterruptionReason("agent_error.provider_auth_or_access")).toBe(false);
    expect(isInterruptionReason("agent_error.provider_quota_limit")).toBe(false);
    expect(isInterruptionReason("agent_error.context_overflow")).toBe(false);
    expect(isInterruptionReason("timeout")).toBe(false);
    expect(isInterruptionReason("iteration_limit")).toBe(false);
    expect(isInterruptionReason("agent_blocked")).toBe(false);
  });

  it("handles the empty/missing wire shape", () => {
    expect(isInterruptionReason("")).toBe(false);
    expect(isInterruptionReason(undefined)).toBe(false);
    expect(isInterruptionReason(null)).toBe(false);
  });
});
