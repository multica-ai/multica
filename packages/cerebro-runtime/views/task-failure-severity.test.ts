import { describe, expect, it } from "vitest";
import { isInterruptionReason } from "./task-failure-severity";

describe("isInterruptionReason", () => {
  it("treats runtime/provider interruptions as interruptions", () => {
    expect(isInterruptionReason("runtime_recovery")).toBe(true);
    expect(isInterruptionReason("runtime_paused")).toBe(true);
    expect(isInterruptionReason("runtime_offline")).toBe(true);
    expect(isInterruptionReason("rate_limit")).toBe(true);
  });

  it("treats genuine failures as non-interruptions", () => {
    expect(isInterruptionReason("agent_error")).toBe(false);
    expect(isInterruptionReason("auth_error")).toBe(false);
    expect(isInterruptionReason("timeout")).toBe(false);
    expect(isInterruptionReason("idle_watchdog")).toBe(false);
  });

  it("handles the empty/missing wire shape", () => {
    expect(isInterruptionReason("")).toBe(false);
    expect(isInterruptionReason(undefined)).toBe(false);
    expect(isInterruptionReason(null)).toBe(false);
  });
});
