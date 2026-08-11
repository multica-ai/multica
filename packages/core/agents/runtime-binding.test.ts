import { describe, expect, it } from "vitest";
import { isAgentRunnable, isAgentRuntimeBound } from "./runtime-binding";

describe("isAgentRuntimeBound", () => {
  it("accepts a bound response from new and old servers", () => {
    expect(
      isAgentRuntimeBound({ runtime_id: "runtime-1", runtime_bound: true }),
    ).toBe(true);
    expect(isAgentRuntimeBound({ runtime_id: "runtime-1" })).toBe(true);
  });

  it("rejects explicit and legacy unbound responses", () => {
    expect(
      isAgentRuntimeBound({ runtime_id: "", runtime_bound: false }),
    ).toBe(false);
    expect(isAgentRuntimeBound({ runtime_id: "" })).toBe(false);
  });

  it("fails closed when additive and legacy signals disagree", () => {
    expect(
      isAgentRuntimeBound({ runtime_id: "runtime-1", runtime_bound: false }),
    ).toBe(false);
    expect(
      isAgentRuntimeBound({ runtime_id: "", runtime_bound: true }),
    ).toBe(false);
  });

  it("fails closed for partial legacy payloads", () => {
    expect(
      isAgentRuntimeBound({
        runtime_id: undefined as unknown as string,
        runtime_bound: true,
      }),
    ).toBe(false);
  });
});

describe("isAgentRunnable", () => {
  it("allows Pool invocation without presenting a Runtime link", () => {
    expect(
		isAgentRunnable({
			runtime_id: "",
			runtime_bound: false,
			runtime_binding_mode: "pool",
			runtime_routable: true,
		}),
	).toBe(true);
	expect(
		isAgentRuntimeBound({ runtime_id: "", runtime_bound: false }),
	).toBe(false);
  });

  it("keeps fixed-unbound Agents unroutable", () => {
	expect(
		isAgentRunnable({
			runtime_id: "",
			runtime_bound: false,
			runtime_binding_mode: "fixed",
			runtime_routable: false,
		}),
	).toBe(false);
  });
});
