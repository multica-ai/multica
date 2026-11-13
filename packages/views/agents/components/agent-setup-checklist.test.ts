// @vitest-environment node

import { describe, expect, it } from "vitest";
import { agentSetupSteps } from "./agent-setup-checklist";

describe("agentSetupSteps", () => {
  it("starts at the runtime when none is connected", () => {
    const steps = agentSetupSteps({ hasRuntime: false, hasAgent: false, hasAssignedIssue: false });
    expect(steps.map((s) => s.id)).toEqual(["runtime", "agent", "issue"]);
    expect(steps.find((s) => s.current)?.id).toBe("runtime");
    expect(steps.every((s) => !s.done)).toBe(true);
  });

  it("moves on to the agent once a runtime is connected", () => {
    const steps = agentSetupSteps({ hasRuntime: true, hasAgent: false, hasAssignedIssue: false });
    expect(steps[0]).toEqual({ id: "runtime", done: true, current: false });
    expect(steps.find((s) => s.current)?.id).toBe("agent");
  });

  it("has no current step when everything is done", () => {
    const steps = agentSetupSteps({ hasRuntime: true, hasAgent: true, hasAssignedIssue: true });
    expect(steps.some((s) => s.current)).toBe(false);
  });
});
