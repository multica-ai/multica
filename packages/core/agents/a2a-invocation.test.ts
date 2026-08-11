import { describe, expect, it } from "vitest";
import {
  ALL_A2A_INVOCATION_SCOPES,
  a2aGrantCount,
  effectiveA2aInvocationScope,
} from "./a2a-invocation";

describe("effectiveA2aInvocationScope", () => {
  it("maps the default mode to disabled", () => {
    expect(effectiveA2aInvocationScope("default")).toBe("disabled");
  });

  it("maps any_agent to any_agent", () => {
    expect(effectiveA2aInvocationScope("any_agent")).toBe("any_agent");
  });

  it("maps squad_leaders to squad_leaders", () => {
    expect(effectiveA2aInvocationScope("squad_leaders")).toBe(
      "squad_leaders",
    );
  });

  it("maps specific_agents to specific_agents", () => {
    expect(effectiveA2aInvocationScope("specific_agents")).toBe(
      "specific_agents",
    );
  });

  it("fails safe to disabled when the mode is absent", () => {
    expect(effectiveA2aInvocationScope(undefined)).toBe("disabled");
    expect(effectiveA2aInvocationScope(null)).toBe("disabled");
  });

  it("fails safe to disabled for an unrecognised (future) mode", () => {
    expect(
      effectiveA2aInvocationScope("future_mode" as never),
    ).toBe("disabled");
  });

  it("fails safe to disabled for a legacy empty string", () => {
    // Old empty-string model is voided (NEX-24 rework): treat it like any
    // unknown value → disabled, never widen.
    expect(effectiveA2aInvocationScope("" as never)).toBe("disabled");
  });
});

describe("ALL_A2A_INVOCATION_SCOPES", () => {
  it("lists the four scopes in display order", () => {
    expect(ALL_A2A_INVOCATION_SCOPES).toEqual([
      "any_agent",
      "squad_leaders",
      "specific_agents",
      "disabled",
    ]);
  });
});

describe("a2aGrantCount", () => {
  it("returns the whitelist length for specific_agents", () => {
    expect(
      a2aGrantCount("specific_agents", ["a-1", "a-2", "a-3"]),
    ).toBe(3);
  });

  it("returns 0 for specific_agents with absent grants", () => {
    expect(a2aGrantCount("specific_agents", undefined)).toBe(0);
    expect(a2aGrantCount("specific_agents", null)).toBe(0);
  });

  it("returns 0 for every non-specific_agents mode even with grants", () => {
    expect(a2aGrantCount("default", ["a-1"])).toBe(0);
    expect(a2aGrantCount("any_agent", ["a-1"])).toBe(0);
    expect(a2aGrantCount("squad_leaders", ["a-1"])).toBe(0);
    expect(a2aGrantCount(undefined, ["a-1"])).toBe(0);
  });
});
