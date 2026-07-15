import { expect, test, type Page, type TestInfo } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { createTestApi, loginAsDefault } from "./helpers";

const HOOK_ID = "22222222-2222-2222-2222-222222222222";
const SHA = process.env.GITHUB_SHA ?? process.env.CI_COMMIT_SHA ?? "local-candidate";
const SHOT_DIR = `e2e/screenshots/fir-3101/${SHA}`;

const basePolicy = {
  id: HOOK_ID,
  version: 2,
  name: "Require a next step",
  description: "No agent stops without a registered continuation",
  mode: "dry_run",
  fail_mode: "closed",
  events: ["before.task.complete"],
  bindings: [{ kind: "model", id: "gpt-5.6" }],
  conditions: [
    { field: "issue.status", op: "not_in", value: "done, cancelled" },
    { field: "continuation", op: "not_exists" },
    { field: "attempt", op: "lt", value: 3 },
  ],
  handlers: [{ id: "primary", decision: "block", requirement: "Choose a valid continuation before completing the task", actions: [{ type: "audit.record", config: {} }] }],
  observed_run_count: 4,
  baseline_at: "2026-07-15T08:00:00Z",
  can_publish: true,
  updated_at: "2026-07-15T08:00:00Z",
};

const run = {
  id: "33333333-3333-3333-3333-333333333333",
  policy_id: HOOK_ID,
  policy_version: 2,
  event: { event_id: "seed-task-complete", event_type: "before.task.complete" },
  source_scope: { kind: "model", id: "gpt-5.6" },
  result: {
    decision: "allow",
    would_decision: "block",
    requirements: ["Choose a valid continuation"],
    matches: [{ policy_id: HOOK_ID, version: 2, handler_id: "primary", source_scope: { kind: "model", id: "gpt-5.6" }, decision: "block", dry_run: true }],
    action_results: [{ type: "audit.record", status: "would_run" }],
  },
  latency_ms: 12,
  created_at: "2026-07-15T08:00:00Z",
};

test.describe("FIR-3101 Workflow Hooks mockup fidelity", () => {
  test.beforeAll(() => mkdirSync(SHOT_DIR, { recursive: true }));

  test.beforeEach(async ({ page }) => {
    const api = await createTestApi();
    await api.setWorkspaceFeatureFlag("cerebro_workflows", true);
    await api.setWorkspaceFeatureFlag("cerebro_workflow_hooks", true);
    await mockHookAPI(page);
    await loginAsDefault(page);
    await page.setViewportSize({ width: 1280, height: 900 });
  });

  test("captures all five approved states with live controls", async ({ page }, testInfo) => {
    const slug = "e2e-workspace";
    await page.goto(`/${slug}/workflows/hooks`);
    await expect(page.getByRole("heading", { name: "Hooks" })).toBeVisible();
    await expect(page.getByRole("button", { name: "New hook" })).toBeVisible();
    await expect(page.getByText("Trigger → 3 conditions → Block → 1 action").first()).toBeVisible();
    for (const state of ["Off", "Dry run", "Enforced", "Managed"]) await expect(page.getByText(state, { exact: true })).toBeVisible();
    await capture(page, testInfo, "01-hooks-overview");

    await page.goto(`/${slug}/workflows/hooks/${HOOK_ID}`);
    const chain = page.getByRole("list", { name: "Hook chain" });
    await expect(chain).toBeVisible();
    await expect(chain.getByRole("button", { name: /Configure/ })).toHaveCount(6);
    await capture(page, testInfo, "02-hook-editor");

    await page.getByRole("button", { name: "Configure Filter" }).click();
    await expect(page.getByLabel("Filter field 1")).toHaveValue("issue.status");
    await expect(page.getByText("AND").first()).toBeVisible();
    await expect(page.getByText(/\{\s*"field"/)).toHaveCount(0);
    await capture(page, testInfo, "03-filter-rows");

    await page.getByRole("button", { name: "Configure Applies to" }).click();
    const scope = page.getByRole("combobox", { name: /^Scope/ });
    for (const value of ["model", "issue", "session"]) {
      await scope.selectOption(value);
      await expect(scope).toHaveValue(value);
    }
    await capture(page, testInfo, "04-shared-scope-chain");

    await page.getByRole("button", { name: "Test" }).click();
    await expect(page.getByRole("region", { name: "Test and history" })).toBeVisible();
    await expect(page.getByText("Test run — no side effects")).toBeVisible();
    await expect(page.getByText("Would block: audit.record")).toBeVisible();
    await capture(page, testInfo, "05-test-history");

    await testInfo.attach("fir-3101-provenance.json", {
      body: Buffer.from(JSON.stringify({ candidate_sha: SHA, route: `/${slug}/workflows/hooks`, viewport: "1280x900", browser: testInfo.project.name, locale: "en", timezone: "UTC", seed: "fir-3101-fixed-v1", capture_command: "pnpm exec playwright test e2e/cerebro-workflow-hooks.spec.ts" }, null, 2)),
      contentType: "application/json",
    });
  });
});

async function capture(page: Page, testInfo: TestInfo, name: string) {
  const path = `${SHOT_DIR}/${name}.png`;
  await expect(page.locator("body")).not.toHaveCSS("overflow-x", "scroll");
  await page.evaluate(() => document.fonts.ready);
  await page.waitForTimeout(150);
  await page.screenshot({ path, fullPage: false });
  await testInfo.attach(name, { path, contentType: "image/png" });
}

async function mockHookAPI(page: Page) {
  await page.route("**/api/cerebro/workflow-hooks**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    if (path.endsWith(`/${HOOK_ID}/runs`)) return route.fulfill({ json: { runs: [run] } });
    if (path.endsWith(`/${HOOK_ID}/test`) && request.method() === "POST") return route.fulfill({ json: { side_effects: false, result: run.result, baseline_at: basePolicy.baseline_at } });
    if (path.endsWith(`/${HOOK_ID}/publish`) && request.method() === "POST") return route.fulfill({ json: { ...basePolicy, mode: "enforce" } });
    if (path.endsWith(`/${HOOK_ID}`)) return route.fulfill({ json: basePolicy });
    if (request.method() === "GET") {
      return route.fulfill({ json: { hooks: [
        { ...basePolicy, id: "00000000-0000-0000-0000-000000000001", name: "Off policy", mode: "off" },
        basePolicy,
        { ...basePolicy, id: "00000000-0000-0000-0000-000000000003", name: "Enforced policy", mode: "enforce" },
        { ...basePolicy, id: "00000000-0000-0000-0000-000000000004", name: "Managed policy", mode: "managed" },
      ] } });
    }
    return route.fulfill({ status: 200, json: basePolicy });
  });
}
