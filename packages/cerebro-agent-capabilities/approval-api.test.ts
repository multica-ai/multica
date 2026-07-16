// FIR-3212 Approval slice — contract tests for the approval-consequences schema.
// Per the API Response Compatibility rules, a malformed or drifting response must
// downgrade to the honest "unknown" state rather than throw into the approval
// queue: an approver whose panel white-screens loses the field diff too, which is
// strictly worse than an approver who is told we cannot say.

import { describe, it, expect } from "vitest";
import { AgentCapabilityApprovalSchema } from "./approval-api";

const wellFormed = {
  agent_id: "agent-1",
  runtime: {
    status: "known",
    provider: "hermes",
    cli_version: "0.4.1",
    runtime_id: "rt-hermes",
    exec_options: [{ field: "model", handling: "honoured", effective: true }],
    silently_ignored: [],
    system_prompt: { native: false, modes: [] },
  },
  impact: {
    status: "known",
    provider: "hermes",
    fields: [
      {
        field: "instructions",
        delivered_by: "engine",
        exec_field: "system_prompt",
        handling: "ignored_logged",
        consequence: "no_effect_logged",
        silent: false,
      },
    ],
    effective: [],
    ineffective: ["instructions"],
    silently_ineffective: [],
    system_prompt: { native: false, modes: [], delivery: "ignored" },
  },
};

describe("AgentCapabilityApprovalSchema", () => {
  it("parses a well-formed approval impact", () => {
    const parsed = AgentCapabilityApprovalSchema.parse(wellFormed);
    expect(parsed.impact.status).toBe("known");
    expect(parsed.impact.fields[0]?.consequence).toBe("no_effect_logged");
    expect(parsed.impact.ineffective).toEqual(["instructions"]);
    expect(parsed.impact.system_prompt?.delivery).toBe("ignored");
    expect(parsed.runtime.provider).toBe("hermes");
  });

  it("defaults a missing impact to status=unknown with nothing enumerated", () => {
    const parsed = AgentCapabilityApprovalSchema.parse({ agent_id: "agent-1" });
    expect(parsed.impact.status).toBe("unknown");
    expect(parsed.impact.fields).toEqual([]);
    expect(parsed.impact.ineffective).toEqual([]);
    expect(parsed.impact.system_prompt).toBeUndefined();
  });

  it("survives a null ineffective array and a wrong-typed flag", () => {
    const parsed = AgentCapabilityApprovalSchema.parse({
      ...wellFormed,
      impact: {
        ...wellFormed.impact,
        ineffective: null,
        fields: [{ ...wellFormed.impact.fields[0], silent: "yes" }],
      },
    });
    expect(parsed.impact.ineffective).toEqual([]);
    expect(parsed.impact.fields[0]?.silent).toBe(false);
  });

  it("keeps an unrecognised consequence instead of crashing (enum drift)", () => {
    const parsed = AgentCapabilityApprovalSchema.parse({
      ...wellFormed,
      impact: {
        ...wellFormed.impact,
        fields: [
          {
            field: "model",
            delivered_by: "telepathy",
            consequence: "quantum",
            silent: false,
          },
        ],
      },
    });
    expect(parsed.impact.fields[0]?.consequence).toBe("quantum");
    expect(parsed.impact.fields[0]?.delivered_by).toBe("telepathy");
  });
});
