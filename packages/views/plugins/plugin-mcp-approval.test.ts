// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { PluginHook, PluginMCPTool } from "@multica/core/types";
import { initialSelection, mcpHooks } from "./plugin-mcp-approval";

function hook(overrides: Partial<PluginHook>): PluginHook {
  return {
    key: "toolbox",
    name: "Toolbox",
    description: "",
    triggers: ["agent"],
    transport: "mcp",
    ...overrides,
  };
}

function tool(overrides: Partial<PluginMCPTool>): PluginMCPTool {
  return { name: "search", description: "", schema_digest: "", approved: false, drifted: false, ...overrides };
}

describe("mcpHooks", () => {
  it("selects only mcp hooks an agent can reach", () => {
    const hooks = [
      hook({ key: "mcp-agent" }),
      hook({ key: "http-agent", transport: "http" }),
      // Approval says WHICH tools; the trigger says whether an agent may reach
      // them at all. A manual-only mcp hook has no agent to approve tools for.
      hook({ key: "mcp-manual", triggers: ["manual"] }),
    ];
    expect(mcpHooks(hooks).map((entry) => entry.key)).toEqual(["mcp-agent"]);
  });

  it("survives a hook whose triggers are missing entirely", () => {
    const malformed = { key: "k", name: "n", description: "", transport: "mcp" } as PluginHook;
    expect(mcpHooks([malformed])).toEqual([]);
  });
});

describe("initialSelection", () => {
  it("checks the approved tools", () => {
    const tools = [tool({ name: "search", approved: true }), tool({ name: "write" })];
    expect(initialSelection(tools)).toEqual(["search"]);
  });

  // The whole reason drift is surfaced instead of silently re-approved: the
  // administrator approved a specific argument shape, and a changed one is a new
  // decision. Leaving the box checked would turn one Save into blind approval.
  it("unchecks an approved tool whose schema drifted", () => {
    const tools = [tool({ name: "search", approved: true, drifted: true })];
    expect(initialSelection(tools)).toEqual([]);
  });
});
