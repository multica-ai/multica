import { expect, test, type Page } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";

const EVAL_ID = "66666666-6666-6666-6666-666666666666";
const WORKFLOW_ID = "77777777-7777-7777-7777-777777777777";
const BINDING_ID = "88888888-8888-8888-8888-888888888888";

const evalItem = {
  id: EVAL_ID, workspace_id: "workspace", key: "answer-quality", version: "1.0.0", title: "Answer quality",
  description: "", status: "active", owner: {}, objective: "Answer correctly", target: { kind: "agent", locator: "mention://agent/lone", ref: "main" },
  datasets: [], graders: [], thresholds: [], runner: {}, source: {}, created_by_id: "member", created_by_type: "member",
  created_at: "2026-07-19T08:00:00Z", updated_at: "2026-07-19T08:00:00Z",
};

const workflow = {
  id: WORKFLOW_ID, workspace_id: "workspace", name: "Support loop", enabled: true, workflow_type: "issue_loop",
  trigger_type: "status_changed", trigger_config: {}, conditions: [], action_type: "set_status", action_config: {}, editor_mode: "form",
  created_by_id: "member", created_by_type: "member", created_at: "2026-07-19T08:00:00Z", updated_at: "2026-07-19T08:00:00Z",
};

test.describe("FIR-3496 Evals v2 connections", () => {
  test.beforeEach(async ({ page }) => {
    const api = await createTestApi();
    await api.setWorkspaceFeatureFlag("cerebro_workflows", true);
    await api.setWorkspaceFeatureFlag("cerebro_evals", true);
    await mockEvalAPI(page);
    await loginAsDefault(page);
  });

  test("binds a monitor eval as warn-only and persists the advisory binding", async ({ page }) => {
    await page.goto("/e2e-workspace/workflows/evals");
    await expect(page.getByRole("heading", { name: "Evals" })).toBeVisible();

    const addGate = page.getByRole("button", { name: "Add advisory gate" });
    await expect(async () => {
      await page.getByLabel("Eval", { exact: true }).selectOption(EVAL_ID);
      await page.getByLabel("Issue workflow").selectOption(WORKFLOW_ID);
      await page.getByLabel("Phase").selectOption("monitor");
      await expect(page.getByLabel("Enforcement")).toHaveValue("warn");
      await expect(addGate).toBeEnabled();
    }).toPass();

    const request = page.waitForRequest((candidate) => candidate.url().endsWith("/api/cerebro/evals/bindings") && candidate.method() === "POST");
    await addGate.click();
    expect((await request).postDataJSON()).toEqual({ workflow_id: WORKFLOW_ID, eval_id: EVAL_ID, phase: "monitor", blocking: false });
    await expect(page.getByText("monitor · Advisory", { exact: false })).toBeVisible();
  });
});

async function mockEvalAPI(page: Page) {
  let bindings: Array<Record<string, unknown>> = [];
  await page.route("**/api/cerebro/workflows", (route) => route.fulfill({ json: { workflows: [workflow] } }));
  await page.route("**/api/cerebro/evals**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.endsWith("/bindings") && request.method() === "POST") {
      const input = request.postDataJSON();
      const binding = { id: BINDING_ID, workspace_id: "workspace", ...input, eval_key: evalItem.key, eval_version: evalItem.version, eval_title: evalItem.title, created_at: "2026-07-19T08:00:00Z" };
      bindings = [binding];
      return route.fulfill({ status: 201, json: binding });
    }
    if (path.endsWith("/bindings")) return route.fulfill({ json: { bindings } });
    if (path.endsWith("/runs")) return route.fulfill({ json: { runs: [] } });
    if (path.endsWith("/schedule")) return route.fulfill({ json: null });
    return route.fulfill({ json: { evals: [evalItem] } });
  });
}
