// FIR-3212 Swap slice — contract tests for the swap-preview schema. Per the API
// Response Compatibility rules, a malformed or drifting response must downgrade
// to the honest "unknown" state rather than throw into the Setup screen, and an
// unknown engine must never render as a total loss of settings.

import { describe, it, expect } from "vitest";
import { AgentCapabilitySwapSchema } from "./swap-api";

const wellFormed = {
  agent_id: "agent-1",
  same_runtime: false,
  current: {
    status: "known",
    provider: "claude",
    cli_version: "2.1.209",
    runtime_id: "rt-claude",
    exec_options: [{ field: "model", handling: "honoured", effective: true }],
    silently_ignored: [],
    system_prompt: { native: true, modes: ["append", "replace"] },
  },
  target: {
    status: "known",
    provider: "kiro",
    cli_version: "0.9.0",
    runtime_id: "rt-kiro",
    exec_options: [{ field: "model", handling: "honoured", effective: true }],
    silently_ignored: ["disallowed_tools"],
    system_prompt: { native: false, modes: ["prepend"] },
  },
  impact: {
    status: "known",
    from: "claude",
    to: "kiro",
    fields: [
      {
        field: "disallowed_tools",
        from_handling: "honoured",
        to_handling: "ignored_silent",
        outcome: "lost",
        silent_on_target: true,
      },
    ],
    lost: ["disallowed_tools"],
    gained: [],
    silently_lost: ["disallowed_tools"],
    system_prompt: {
      from_native: true,
      to_native: false,
      from_modes: ["append", "replace"],
      to_modes: ["prepend"],
      outcome: "downgraded_to_prepend",
    },
  },
};

describe("AgentCapabilitySwapSchema", () => {
  it("parses a well-formed swap preview", () => {
    const parsed = AgentCapabilitySwapSchema.parse(wellFormed);
    expect(parsed.impact.status).toBe("known");
    expect(parsed.impact.fields[0]?.silent_on_target).toBe(true);
    expect(parsed.impact.system_prompt?.outcome).toBe("downgraded_to_prepend");
    expect(parsed.target.provider).toBe("kiro");
  });

  it("defaults a missing impact to status=unknown with nothing enumerated", () => {
    const parsed = AgentCapabilitySwapSchema.parse({ agent_id: "agent-1" });
    expect(parsed.impact.status).toBe("unknown");
    expect(parsed.impact.fields).toEqual([]);
    expect(parsed.impact.lost).toEqual([]);
    expect(parsed.impact.system_prompt).toBeUndefined();
  });

  it("survives a null lost array and a wrong-typed flag", () => {
    const parsed = AgentCapabilitySwapSchema.parse({
      ...wellFormed,
      same_runtime: "yes",
      impact: { ...wellFormed.impact, lost: null },
    });
    expect(parsed.same_runtime).toBe(false);
    expect(parsed.impact.lost).toEqual([]);
  });

  it("keeps an unrecognised outcome instead of crashing (enum drift)", () => {
    const parsed = AgentCapabilitySwapSchema.parse({
      ...wellFormed,
      impact: {
        ...wellFormed.impact,
        fields: [
          {
            field: "model",
            from_handling: "honoured",
            to_handling: "teleported",
            outcome: "quantum",
            silent_on_target: false,
          },
        ],
      },
    });
    expect(parsed.impact.fields[0]?.outcome).toBe("quantum");
  });
});
