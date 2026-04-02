import { describe, expect, it } from "vitest";

import { buildAgentInstructionStarter } from "@/features/agents/instruction-starter";

describe("buildAgentInstructionStarter", () => {
  it("uses the agent name and normalizes the description into the intro", () => {
    const starter = buildAgentInstructionStarter({
      name: "Research Assistant",
      description: "helps with issue triage and debugging",
    });

    expect(starter).toContain(
      "You are Research Assistant. Helps with issue triage and debugging.",
    );
    expect(starter).toContain("## Collaboration Style");
    expect(starter).toContain("## Guardrails");
  });

  it("falls back to a generic intro when description is empty", () => {
    const starter = buildAgentInstructionStarter({ name: "小助理" });

    expect(starter).toContain("You are 小助理.");
    expect(starter).toContain("pragmatic coding assistant");
  });
});
