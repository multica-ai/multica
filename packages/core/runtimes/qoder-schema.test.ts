// @vitest-environment node
import { describe, it, expect } from "vitest";
import { parseQoderConnection, parseQoderCatalog } from "./qoder-schema";
describe("QCA response boundary", () => {
  it("disables configuration for malformed responses", () => {
    for (const input of [
      null,
      {},
      { available: "true" },
      { repositories: "bad" },
    ]) {
      expect(parseQoderConnection(input).available).toBe(false);
    }
  });
  it("preserves capability and valid fields when optional fields are missing", () => {
    expect(parseQoderConnection({ available: true, configured: true, name: "QCA", repositories: null })).toMatchObject({ available: true, configured: true, name: "QCA", repositories: [], enabled: false });
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
  it("maps execution limits and usage, defaulting malformed values", () => {
    expect(
      parseQoderConnection({
        available: true,
        max_concurrency: 5,
        run_timeout_minutes: 90,
        active_runs: 2,
        queued_runs: 1,
      }),
    ).toMatchObject({
      maxConcurrency: 5,
      runTimeoutMinutes: 90,
      activeRuns: 2,
      queuedRuns: 1,
    });
    expect(
      parseQoderConnection({
        available: true,
        max_concurrency: 0,
        run_timeout_minutes: "60",
        active_runs: -1,
      }),
    ).toMatchObject({
      available: true,
      maxConcurrency: 3,
      runTimeoutMinutes: 60,
      activeRuns: 0,
      queuedRuns: 0,
    });
  });
});


it("validates catalogs and strips provider secrets", () => {
  expect(parseQoderCatalog({ items: [{ id: "env_a", name: "Web", config: { token: "secret" } }] })).toEqual([{ id: "env_a", name: "Web" }]);
  for (const malformed of [null, {}, { items: "bad" }, { items: [{ id: 1, name: "bad" }] }]) {
    expect(parseQoderCatalog(malformed)).toEqual([]);
  }
});
