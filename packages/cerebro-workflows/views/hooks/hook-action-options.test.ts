// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { ACTION_OPTIONS } from "./hook-step-panel";

describe("Hook action options (FIR-3496)", () => {
  it("offers eval.run and eval.gate in the action dropdown", () => {
    expect(ACTION_OPTIONS).toContainEqual({ value: "eval.run", label: "Run eval" });
    expect(ACTION_OPTIONS).toContainEqual({ value: "eval.gate", label: "Eval gate" });
  });
});
