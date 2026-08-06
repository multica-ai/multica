import { describe, expect, it } from "vitest";
import type { RuntimeModel } from "@multica/core/types";
import { buildModelChangeUpdate } from "./model-change-cleanup";

const FAST_HIGH: RuntimeModel = {
  id: "gpt-5.6-sol",
  label: "GPT-5.6 Sol",
  thinking: {
    supported_levels: [
      { value: "medium", label: "Medium" },
      { value: "high", label: "High" },
    ],
  },
  service_tiers: [{ id: "priority", name: "Fast" }],
};

const PLAIN: RuntimeModel = {
  id: "gpt-5.4-mini",
  label: "GPT-5.4 mini",
};

const MEDIUM_ONLY: RuntimeModel = {
  id: "gpt-5.5",
  label: "GPT-5.5",
  thinking: { supported_levels: [{ value: "medium", label: "Medium" }] },
  service_tiers: [{ id: "priority", name: "Fast" }],
};

const CATALOG = [FAST_HIGH, PLAIN, MEDIUM_ONLY];

describe("buildModelChangeUpdate (MUL-5390)", () => {
  it("clears overrides the new model does not advertise", () => {
    expect(
      buildModelChangeUpdate({
        model: "gpt-5.4-mini",
        thinkingLevel: "high",
        serviceTier: "priority",
        catalog: CATALOG,
      }),
    ).toEqual({
      model: "gpt-5.4-mini",
      thinking_level: "",
      service_tier: "",
    });
  });

  it("keeps overrides the new model still supports", () => {
    expect(
      buildModelChangeUpdate({
        model: "gpt-5.6-sol",
        thinkingLevel: "high",
        serviceTier: "priority",
        catalog: CATALOG,
      }),
    ).toEqual({ model: "gpt-5.6-sol" });
  });

  it("clears only the unsupported half", () => {
    expect(
      buildModelChangeUpdate({
        model: "gpt-5.5",
        thinkingLevel: "high",
        serviceTier: "priority",
        catalog: CATALOG,
      }),
    ).toEqual({ model: "gpt-5.5", thinking_level: "" });
  });

  // Clearing on an unknown catalog would delete a value the daemon would have
  // honoured, so an offline runtime / in-flight or failed discovery only writes
  // the model.
  it("leaves overrides untouched when the catalog is not authoritative", () => {
    expect(
      buildModelChangeUpdate({
        model: "gpt-5.4-mini",
        thinkingLevel: "high",
        serviceTier: "priority",
        catalog: null,
      }),
    ).toEqual({ model: "gpt-5.4-mini" });
  });

  it("leaves overrides untouched for an unresolvable runtime-default model", () => {
    expect(
      buildModelChangeUpdate({
        model: "",
        thinkingLevel: "high",
        serviceTier: "priority",
        catalog: CATALOG,
      }),
    ).toEqual({ model: "" });
  });

  it("leaves overrides untouched for a model missing from the catalog", () => {
    expect(
      buildModelChangeUpdate({
        model: "custom-local-build",
        thinkingLevel: "high",
        serviceTier: "priority",
        catalog: CATALOG,
      }),
    ).toEqual({ model: "custom-local-build" });
  });

  it("never sends a clear when nothing is set", () => {
    expect(
      buildModelChangeUpdate({
        model: "gpt-5.4-mini",
        thinkingLevel: "",
        serviceTier: "",
        catalog: CATALOG,
      }),
    ).toEqual({ model: "gpt-5.4-mini" });
  });
});
