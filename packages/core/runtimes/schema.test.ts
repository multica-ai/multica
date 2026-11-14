// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "../api/schema";
import { AgentRuntimeSchema, EMPTY_RUNTIME } from "./schema";

describe("runtime sharing response contract", () => {
  it("preserves grants from a new server and defaults missing grants to empty", () => {
    const member = "11111111-1111-4111-8111-111111111111";
    expect(AgentRuntimeSchema.parse({ id: "r", workspace_id: "w", shared_with: [member] }).shared_with).toEqual([member]);
    expect(AgentRuntimeSchema.parse({ id: "r", workspace_id: "w" }).shared_with).toEqual([]);
  });
  it.each([null, "all", [123], ["not-a-uuid"]])("fails closed for malformed grants: %j", (shared_with) => {
    expect(AgentRuntimeSchema.parse({ id: "r", workspace_id: "w", shared_with }).shared_with).toEqual([]);
  });
  it("falls back safely for a malformed runtime", () => {
    expect(parseWithFallback({ id: 123 }, AgentRuntimeSchema, EMPTY_RUNTIME, { endpoint: "test" })).toEqual(EMPTY_RUNTIME);
  });
});

describe("runtime concurrency cap response contract", () => {
  it("preserves a cap from a new server", () => {
    expect(AgentRuntimeSchema.parse({ id: "r", workspace_id: "w", max_concurrent_tasks: 2 }).max_concurrent_tasks).toBe(2);
  });
  it("treats an older server that omits the field as no cap", () => {
    expect(AgentRuntimeSchema.parse({ id: "r", workspace_id: "w" }).max_concurrent_tasks).toBeNull();
  });
  it("keeps an explicit null as no cap", () => {
    expect(AgentRuntimeSchema.parse({ id: "r", workspace_id: "w", max_concurrent_tasks: null }).max_concurrent_tasks).toBeNull();
  });
  it.each(["2", 1.5, true, {}, [1]])("degrades a malformed cap to no cap without dropping the runtime: %j", (max_concurrent_tasks) => {
    const parsed = AgentRuntimeSchema.parse({ id: "r", workspace_id: "w", name: "Claude", max_concurrent_tasks });
    expect(parsed.max_concurrent_tasks).toBeNull();
    expect(parsed.name).toBe("Claude");
  });
});

describe("runtime quota response contract", () => {
  it("preserves zero usage and does not invent a second window", () => {
    const snapshot = { observedAt: 1_800_000_000, primary: { usedPercent: 0, windowDurationMins: 180 } };
    const runtime = AgentRuntimeSchema.parse({ id: "r", workspace_id: "w", subscription_quota: snapshot });
    expect(runtime.subscription_quota).toEqual(snapshot);
  });

  it.each([
    null,
    { observedAt: 1_800_000_000 },
    { observedAt: 1_800_000_000, primary: { usedPercent: 120 } },
    { observedAt: Number.MAX_SAFE_INTEGER, primary: { usedPercent: 0 } },
  ])("drops invalid quota without dropping the runtime: %j", (subscription_quota) => {
    const runtime = AgentRuntimeSchema.parse({ id: "r", workspace_id: "w", subscription_quota });
    expect(runtime.id).toBe("r");
    expect(runtime.subscription_quota).toBeUndefined();
  });
});
