import { describe, expect, it } from "vitest";
import { advancedTabIds, agentPageTabIds } from "./agent-page-tabs";

describe("agentPageTabIds", () => {
  // Tools / Capabilities / Memory / Production prompt / Quality are not
  // asserted here — FIR-4840 moved them to @multica/cerebro-agent-tabs, which
  // owns its own coverage.
  it("keeps the setup tabs this page owns itself", () => {
    expect(agentPageTabIds({ mcpConfig: true, integrations: true })).toEqual([
      "tasks",
      "instructions",
      "skills",
      "advanced",
      "integrations",
    ]);
  });

  it("hides runtime-dependent tabs when their backing feature is unavailable", () => {
    expect(agentPageTabIds({ mcpConfig: false, integrations: false })).toEqual([
      "tasks",
      "instructions",
      "skills",
      "advanced",
    ]);
  });

  it("groups runtime settings in Advanced in their requested order", () => {
    expect(advancedTabIds({ mcpConfig: true })).toEqual([
      "infisical",
      "mcp_config",
      "custom_args",
      "env",
    ]);
    expect(advancedTabIds({ mcpConfig: false })).toEqual([
      "infisical",
      "custom_args",
      "env",
    ]);
  });
});
