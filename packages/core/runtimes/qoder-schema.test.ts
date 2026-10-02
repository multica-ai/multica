// @vitest-environment node
import { describe, it, expect } from "vitest";
import { parseQoderConnection, parseQoderCatalog } from "./qoder-schema";
describe("QCA response boundary", () => {
  it("disables configuration for malformed responses", () => {
    for (const input of [
      null,
      {},
      { available: true },
      { repositories: "bad" },
    ]) {
      expect(parseQoderConnection(input).available).toBe(false);
    }
  });
  it("maps fields and drops unexpected secrets", () => {
    const result = parseQoderConnection({
      configured: true,
      available: true,
      name: "QCA",
      base_url: "https://api.qoder.com/api/v1/cloud",
      agent_id: "agent_a",
      environment_id: "env_a",
      has_token: true,
      repositories: ["https://github.com/a/b"],
      enabled: true,
      status: "online",
      last_error: "",
      qoder_token: "must-not-surface",
    });
    expect(result.environmentId).toBe("env_a");
    expect(result).not.toHaveProperty("agentId");
    expect(result.hasToken).toBe(true);
    expect(JSON.stringify(result)).not.toContain("must-not-surface");
  });
});


it("validates catalogs and strips provider secrets", () => {
  expect(parseQoderCatalog({ items: [{ id: "env_a", name: "Web", config: { token: "secret" } }] })).toEqual([{ id: "env_a", name: "Web" }]);
  for (const malformed of [null, {}, { items: "bad" }, { items: [{ id: 1, name: "bad" }] }]) {
    expect(parseQoderCatalog(malformed)).toEqual([]);
  }
});
