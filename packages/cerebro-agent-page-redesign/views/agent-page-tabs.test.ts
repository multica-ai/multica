import { describe, expect, it } from "vitest";
import { agentPageTabIds } from "./agent-page-tabs";

describe("agentPageTabIds", () => {
  it("keeps the shipped setup tabs from the old agent detail page", () => {
    expect(agentPageTabIds({ mcpConfig: true, integrations: true })).toEqual([
      "tasks",
      "instructions",
      "skills",
      "env",
      "infisical",
      "custom_args",
      "sandbox",
      "mcp_config",
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
      "env",
      "infisical",
      "custom_args",
      "sandbox",
      "tools",
      "capabilities",
      "memory",
    ]);
  });
});
