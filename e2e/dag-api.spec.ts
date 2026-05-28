import { test, expect } from "@playwright/test";
import { createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

// API-only DAG core E2E tests.
// These validate the DAG backend endpoints without requiring frontend navigation.
// The Playwright test runner provides test isolation; actual assertions use fetch.

test.describe("DAG Core API", () => {
  let api: TestApiClient;

  test.beforeEach(async () => {
    api = await createTestApi();
  });

  test.afterEach(async () => {
    if (api) {
      await api.cleanup();
    }
  });

  test("dag analysis returns graph metrics for workspace", async () => {
    const res = await api.authedFetch("/api/dag/analysis");
    expect(res.ok).toBe(true);

    const data = await res.json();
    expect(data).toHaveProperty("node_count");
    expect(data).toHaveProperty("edge_count");
    expect(data).toHaveProperty("cycles");
    expect(data).toHaveProperty("status");
    expect(data.status).toBe("computed");
    expect(Array.isArray(data.cycles)).toBe(true);
  });

  test("dag event append accepts valid event", async () => {
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

    expect(res.status).toBe(201);
    const data = await res.json();
    expect(data.status).toBe("appended");
    expect(data.event_id).toBeDefined();
  });

  test("dag event append rejects invalid event", async () => {
    const event = {
      event: {
        id: "e2e-invalid-" + Date.now(),
        record_ids: ["rec-1"],
        operation: "create",
        // missing agent_id
      },
    };

    const res = await api.authedFetch("/api/dag/events", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(event),
    });

    expect(res.status).toBe(400);
  });

  test("dag analysis reflects created records", async () => {
    const eventId = "e2e-reflect-" + Date.now();
    const createRes = await api.authedFetch("/api/dag/events", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        event: {
          id: eventId,
          record_ids: ["rec-reflect-1"],
          agent_id: "e2e-agent",
          dvt: {
            dot: { agent_id: "e2e-agent", counter: 1 },
            context: { "e2e-agent": 1 },
          },
          operation: "create",
          payload: { type: "issue" },
          reason: "e2e reflect test",
        },
      }),
    });
    expect(createRes.status).toBe(201);

    const analysisRes = await api.authedFetch("/api/dag/analysis");
    expect(analysisRes.ok).toBe(true);

    const data = await analysisRes.json();
    expect(data.node_count).toBeGreaterThanOrEqual(1);
  });
});
