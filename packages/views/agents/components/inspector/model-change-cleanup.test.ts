// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { RuntimeModel } from "@multica/core/types";
import { buildModelChangeUpdate } from "./model-change-cleanup";

const FAST_HIGH: RuntimeModel = {
  id: "gpt-5.6-sol",
  label: "GPT-5.6 Sol",
  supports_explicit_standard_service_tier: true,
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
  supports_explicit_standard_service_tier: true,
};

const MEDIUM_ONLY: RuntimeModel = {
  id: "gpt-5.5",
  label: "GPT-5.5",
  supports_explicit_standard_service_tier: true,
  thinking: { supported_levels: [{ value: "medium", label: "Medium" }] },
  service_tiers: [{ id: "priority", name: "Fast" }],
};

const CATALOG = [FAST_HIGH, PLAIN, MEDIUM_ONLY];

const CLAUDE_MEDIUM_ONLY: RuntimeModel = {
  id: "claude-sonnet-4-6",
  label: "Claude Sonnet 4.6",
  thinking: { supported_levels: [{ value: "medium", label: "Medium" }] },
};

// zcode's registry always pairs levels with a default; the composite id shape
// matches what discovery reports (providerId/modelId).
const ZCODE_GLM: RuntimeModel = {
  id: "zai-api/GLM-5.3",
  label: "GLM-5.3",
  thinking: {
    supported_levels: [
      { value: "low", label: "low" },
      { value: "max", label: "max" },
    ],
    default_level: "max",
  },
};

describe("buildModelChangeUpdate (MUL-5390)", () => {
  it("clears overrides the new model does not advertise", () => {
    expect(
      buildModelChangeUpdate({
        provider: "codex",
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
        provider: "codex",
        model: "gpt-5.6-sol",
        thinkingLevel: "high",
        serviceTier: "priority",
        catalog: CATALOG,
      }),
    ).toEqual({ model: "gpt-5.6-sol" });
  });

  it("keeps Codex explicit standard across model changes", () => {
    expect(
      buildModelChangeUpdate({
        provider: "codex",
        model: "gpt-5.4-mini",
        thinkingLevel: "",
        serviceTier: "default",
        catalog: CATALOG,
      }),
    ).toEqual({ model: "gpt-5.4-mini" });
  });

  it("clears explicit standard when an older daemon does not advertise support", () => {
    expect(
      buildModelChangeUpdate({
        provider: "codex",
        model: "gpt-5.4-mini",
        thinkingLevel: "",
        serviceTier: "default",
        catalog: [{ id: "gpt-5.4-mini", label: "GPT-5.4 mini" }],
      }),
    ).toEqual({ model: "gpt-5.4-mini", service_tier: "" });
  });

  it("clears only the unsupported half", () => {
    expect(
      buildModelChangeUpdate({
        provider: "codex",
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
        provider: "codex",
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
        provider: "codex",
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
        provider: "codex",
        model: "custom-local-build",
        thinkingLevel: "high",
        serviceTier: "priority",
        catalog: CATALOG,
      }),
    ).toEqual({ model: "custom-local-build" });
  });

  it("clears an unsupported override on a context-tagged Claude model", () => {
    expect(
      buildModelChangeUpdate({
        provider: "claude",
        model: "claude-sonnet-4-6[1m]",
        thinkingLevel: "xhigh",
        serviceTier: "",
        catalog: [CLAUDE_MEDIUM_ONLY],
      }),
    ).toEqual({
      model: "claude-sonnet-4-6[1m]",
      thinking_level: "",
    });
  });

  it("never sends a clear when nothing is set", () => {
    expect(
      buildModelChangeUpdate({
        provider: "codex",
        model: "gpt-5.4-mini",
        thinkingLevel: "",
        serviceTier: "",
        catalog: CATALOG,
      }),
    ).toEqual({ model: "gpt-5.4-mini" });
  });

  // zcode's session/create rejects a reasoning-capable selection without an
  // explicit level, so an empty stored level must never survive a model change.
  it("prefills the catalog default for zcode when no level is stored", () => {
    expect(
      buildModelChangeUpdate({
        provider: "zcode",
        model: "zai-api/GLM-5.3",
        thinkingLevel: "",
        serviceTier: "",
        catalog: [ZCODE_GLM],
      }),
    ).toEqual({ model: "zai-api/GLM-5.3", thinking_level: "max" });
  });

  it("resolves an unsupported zcode level to the catalog default, not empty", () => {
    expect(
      buildModelChangeUpdate({
        provider: "zcode",
        model: "zai-api/GLM-5.3",
        thinkingLevel: "ultra",
        serviceTier: "",
        catalog: [ZCODE_GLM],
      }),
    ).toEqual({ model: "zai-api/GLM-5.3", thinking_level: "max" });
  });

  it("keeps a still-supported zcode level on model change", () => {
    expect(
      buildModelChangeUpdate({
        provider: "zcode",
        model: "zai-api/GLM-5.3",
        thinkingLevel: "low",
        serviceTier: "",
        catalog: [ZCODE_GLM],
      }),
    ).toEqual({ model: "zai-api/GLM-5.3" });
  });

  it("leaves a zcode level unset when the catalog carries no default", () => {
    expect(
      buildModelChangeUpdate({
        provider: "zcode",
        model: "zai-api/GLM-5.3",
        thinkingLevel: "",
        serviceTier: "",
        catalog: [
          {
            ...ZCODE_GLM,
            thinking: { supported_levels: ZCODE_GLM.thinking!.supported_levels },
          },
        ],
      }),
    ).toEqual({ model: "zai-api/GLM-5.3" });
  });
});
