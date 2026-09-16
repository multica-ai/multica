// @vitest-environment node
import { describe, expect, it } from "vitest";
import { usageCoverage } from "./usage-coverage";

describe("per-run usage coverage", () => {
  it.each([
    [undefined, "unknown"],
    [[], "unknown"],
    [["unknown"], "unknown"],
    [["future_source"], "unknown"],
    [["final_model_usage", "future_source"], "unknown"],
    [["final_model_usage"], "all_agents"],
    [["final_model_usage", "final_model_usage"], "all_agents"],
    [["final_usage"], "main_agent"],
    [["final_model_usage", "final_usage"], "main_agent"],
    [["assistant_fallback"], "partial"],
    [["assistant_fallback", "final_model_usage"], "partial"],
    [["final_model_usage", "assistant_fallback"], "partial"],
    [["none", "final_model_usage"], "partial"],
    [["assistant_fallback", "unknown"], "partial"],
    [["none"], "unavailable"],
    [["none", "none"], "unavailable"],
  ])("%j is %s", (sources, expected) => {
    expect(usageCoverage(sources as string[] | undefined)).toBe(expected);
  });
});
