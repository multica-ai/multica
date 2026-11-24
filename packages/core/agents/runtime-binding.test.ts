// @vitest-environment node
import { describe, expect, it } from "vitest";
import { isAgentRuntimeBound } from "./runtime-binding";

describe("isAgentRuntimeBound", () => {
  it("accepts a bound response from new and old servers", () => {
    expect(
      isAgentRuntimeBound({ runtime_id: "runtime-1", runtime_bound: true }),
    ).toBe(true);
    expect(isAgentRuntimeBound({ runtime_id: "runtime-1" })).toBe(true);
  });

  it("accepts a personal execution binding independently of the shared default", () => {
    expect(isAgentRuntimeBound({ runtime_id: "", runtime_bound: false, personal_runtime_id: "mine" })).toBe(true);
    expect(isAgentRuntimeBound({ runtime_id: "", runtime_bound: false, personal_runtime_id: "" })).toBe(false);
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
