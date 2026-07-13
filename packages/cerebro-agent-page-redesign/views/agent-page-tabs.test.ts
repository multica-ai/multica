import { describe, expect, it } from "vitest";
import { advancedTabIds, agentPageTabIds } from "./agent-page-tabs";

describe("agentPageTabIds", () => {
  it("keeps the shipped setup tabs from the old agent detail page", () => {
    expect(agentPageTabIds({ mcpConfig: true, integrations: true })).toEqual([
      "tasks",
      "instructions",
      "skills",
      "advanced",
      "integrations",
      "tools",
      "capabilities",
      "memory",
    ]);
  });

  it("hides runtime-dependent tabs when their backing feature is unavailable", () => {
    expect(agentPageTabIds({ mcpConfig: false, integrations: false })).toEqual([
      "tasks",
      "instructions",
      "skills",
      "advanced",
      "tools",
      "capabilities",
      "memory",
    ]);
  });

  it("groups runtime settings in Advanced in their requested order", () => {
    expect(advancedTabIds({ mcpConfig: true })).toEqual([
      "infisical",
      "sandbox",
      "mcp_config",
      "custom_args",
      "env",
    ]);
    expect(advancedTabIds({ mcpConfig: false })).toEqual([
      "infisical",
      "sandbox",
      "custom_args",
      "env",
    ]);
  });
});
