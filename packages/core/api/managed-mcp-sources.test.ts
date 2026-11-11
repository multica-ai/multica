// @vitest-environment node
import { describe, expect, it } from "vitest";
import { AgentTaskSchema } from "./schemas";

describe("managed MCP source projection", () => {
  it("keeps old history absent and a newly captured empty set distinct", () => {
    expect(AgentTaskSchema.parse({ id: "run" }).managed_mcp_sources).toBeUndefined();
    expect(AgentTaskSchema.parse({ id: "run", managed_mcp_sources: [] }).managed_mcp_sources).toEqual([]);
  });

  it("accepts only fixed source labels and name-only entries", () => {
    const sources = [{ name: "shared", source: "workspace" }, { name: "own", source: "agent" }, { name: "apps", source: "task" }];
    expect(AgentTaskSchema.parse({ id: "run", managed_mcp_sources: sources }).managed_mcp_sources).toEqual(sources);
    for (const bad of [null, {}, [{ name: "", source: "agent" }], [{ name: "a", source: "https://secret.example" }], [{ name: "a", source: "agent", token: "SECRET_CANARY" }]]) {
      const task = AgentTaskSchema.parse({ id: "run", managed_mcp_sources: bad });
      expect(task.id).toBe("run");
      expect(task.managed_mcp_sources).toBeUndefined();
      expect(JSON.stringify(task)).not.toContain("SECRET_CANARY");
    }
  });
});
