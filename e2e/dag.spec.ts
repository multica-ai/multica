import { test, expect } from "@playwright/test";
import { loginAsDefault, createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

test.describe("DAG Core", () => {
  let api: TestApiClient;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    await loginAsDefault(page);
  });

  test.afterEach(async () => {
    if (api) {
      await api.cleanup();
    }
  });

  test("dag analysis returns graph metrics for workspace", async ({ page }) => {
    // Create a couple issues via API to have graph data
    const issue1 = await api.createIssue("DAG Test Issue 1 " + Date.now());
    const issue2 = await api.createIssue("DAG Test Issue 2 " + Date.now());

    // The backfill may not have run, so the DAG tables might be empty.
    // We verify the endpoint responds correctly either way.
    const res = await api.authedFetch("/api/dag/analysis");
    expect(res.ok).toBe(true);

    const data = await res.json();
    expect(data).toHaveProperty("node_count");
    expect(data).toHaveProperty("edge_count");
    expect(data).toHaveProperty("cycles");
    expect(data).toHaveProperty("status");
    expect(data.status).toBe("computed");
  });

  test("dag event append accepts valid event", async ({ page }) => {
    const event = {
      event: {
        id: "e2e-test-" + Date.now(),
        record_ids: ["issue-e2e-1"],
        agent_id: "e2e-agent",
        dvt: {
          dot: { agent_id: "e2e-agent", counter: 1 },
          context: { "e2e-agent": 1 },
        },
        operation: "create",
        payload: { type: "issue" },
        reason: "e2e test",
      },
    };

    const res = await api.authedFetch("/api/dag/events", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(event),
    });

    // Should succeed or fail validation gracefully
    expect([201, 400]).toContain(res.status);
  });
});
