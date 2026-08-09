// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { ACTION_CONFIGURATION } from "../../core/hook-ux";
import { ACTION_OPTIONS } from "./hook-step-panel";

describe("Hook action options (FIR-3496)", () => {
  it("derives every picker option from the shared action catalog", () => {
    expect(ACTION_OPTIONS.map((action) => action.value)).toEqual(Object.keys(ACTION_CONFIGURATION));
  });

  it("offers eval.run and eval.gate in the action dropdown", () => {
    expect(ACTION_OPTIONS).toContainEqual({ value: "eval.run", label: "Run eval" });
    expect(ACTION_OPTIONS).toContainEqual({ value: "eval.gate", label: "Eval gate" });
  });
});
