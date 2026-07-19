import { expect, test, type Page, type TestInfo } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { createTestApi, loginAsDefault } from "./helpers";

const HOOK_ID = "22222222-2222-2222-2222-222222222222";
const ENFORCED_HOOK_ID = "00000000-0000-0000-0000-000000000003";
const NEW_HOOK_ID = "55555555-5555-5555-5555-555555555555";
const EVAL_ID = "66666666-6666-6666-6666-666666666666";
const SHA = process.env.GITHUB_SHA ?? process.env.CI_COMMIT_SHA ?? "local-candidate";
const SHOT_DIR = `e2e/screenshots/fir-3321/${SHA}`;

const basePolicy = {
  id: HOOK_ID,
  version: 2,
  name: "Require a next step",
  description: "No agent stops without a registered continuation",
  mode: "dry_run",
  fail_mode: "closed",
  events: ["before.task.complete"],
  bindings: [{ kind: "workspace", id: "" }],
  conditions: [
    { field: "issue.status", op: "not_in", values: ["done", "cancelled"] },
    { field: "continuation", op: "not_exists" },
    { field: "attempt", op: "lt", value: 3 },
  ],
  handlers: [{ id: "primary", decision: "block", requirement: "Choose a valid continuation before completing the task", actions: [{ type: "audit.record", config: { event: "continuation_required" } }] }],
  observed_run_count: 4,
  baseline_at: "2026-07-15T08:00:00Z",
  can_publish: true,
  updated_at: "2026-07-15T08:00:00Z",
  last_run_at: "2026-07-15T08:00:00Z",
};

const run = {
  id: "33333333-3333-3333-3333-333333333333",
  policy_id: HOOK_ID,
  policy_version: 2,
  event: { event_id: "seed-task-complete", event_type: "before.task.complete" },
  source_scope: { kind: "workspace", id: "" },
  result: {
    decision: "allow",
    would_decision: "block",
    requirements: ["Choose a valid continuation"],
    matches: [{ policy_id: HOOK_ID, version: 2, handler_id: "primary", source_scope: { kind: "workspace", id: "" }, decision: "block", dry_run: true }],
    action_results: [{ type: "audit.record", status: "would_run" }],
  },
  latency_ms: 12,
  created_at: "2026-07-15T08:00:00Z",
};

test.describe("FIR-3321 Workflow Hooks delivery", () => {
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
    await expect(page.getByText(/This hook runs before a task completes/).first()).toBeVisible();
    for (const state of ["Off", "Dry run", "Enforced", "Managed"]) await expect(page.getByText(state, { exact: true })).toBeVisible();
    await capture(page, testInfo, "01-hooks-overview");

    await page.goto(`/${slug}/workflows/hooks/${HOOK_ID}`);
    const chain = page.getByRole("list", { name: "Hook chain" });
    await expect(chain).toBeVisible();
    await expect(chain.getByRole("button", { name: /Configure/ })).toHaveCount(3);
    await expect(chain.getByRole("button", { name: "Configure When" })).toBeVisible();
    await expect(chain.getByRole("button", { name: "Configure Guide or enforce" })).toBeVisible();
    await expect(chain.getByRole("button", { name: "Configure Actions" })).toBeVisible();
    await capture(page, testInfo, "02-hook-editor");

    await page.getByRole("button", { name: "Configure When" }).click();
    await expect(page.getByLabel("Filter field 1")).toContainText("Issue status");
    await expect(page.getByText("All of the following must match")).toBeVisible();
    await expect(page.getByLabel("Filter conjunction 2")).toHaveCount(0);
    await expect(page.getByText(/\{\s*"field"/)).toHaveCount(0);
    await capture(page, testInfo, "03-filter-rows");

    const scope = page.getByRole("combobox", { name: "Scope 1" });
    for (const value of ["model", "issue", "session"]) {
      await scope.click();
      await page.getByRole("option", { name: value === "model" ? "Model" : value === "issue" ? "Issue" : "Chat or session" }).click();
      await expect(scope).toContainText(value === "model" ? "Model" : value === "issue" ? "Issue" : "Chat or session");
    }
    await capture(page, testInfo, "04-shared-scope-chain");

    await page.getByRole("button", { name: "Test" }).click();
    await expect(page.getByRole("region", { name: "Test and history" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Run test with selected event" })).toBeEnabled();
    await expect(page.getByText("Test run — no side effects")).toBeVisible();
    await expect(page.getByText("Would block: audit.record")).toBeVisible();
    await capture(page, testInfo, "05-test-history");

    await testInfo.attach("fir-3321-provenance.json", {
      body: Buffer.from(JSON.stringify({ candidate_sha: SHA, route: `/${slug}/workflows/hooks`, viewport: "1280x900", browser: testInfo.project.name, locale: "en", timezone: "UTC", seed: "fir-3321-fixed-v1", capture_command: "pnpm exec playwright test e2e/cerebro-workflow-hooks.spec.ts" }, null, 2)),
      contentType: "application/json",
    });
  });

  test("starts empty and exposes typed target actions", async ({ page }, testInfo) => {
    const slug = "e2e-workspace";
    await page.goto(`/${slug}/workflows/hooks/new`);
    await expect(page.getByRole("heading", { name: "Start with a proven recipe" })).toBeVisible();
    await expect(page.getByRole("button", { name: /Require a continuation before completion/ })).toBeVisible();
    await capture(page, testInfo, "00-template-picker");
    await page.getByRole("button", { name: /Start from scratch/ }).click();
    await expect(page.getByLabel("Hook name")).toHaveValue("");
    await expect(page.getByRole("button", { name: "Publish" })).toBeDisabled();
    await expect(page.getByRole("button", { name: "Configure When" })).toHaveAttribute("data-state", "incomplete");

    await page.getByRole("button", { name: "Configure Actions" }).click();
    await page.getByRole("button", { name: "Add action", exact: true }).click();
    await page.getByLabel("Action 1 type").click();
    await expect(page.getByRole("option", { name: "Start agent" })).toBeVisible();
    await expect(page.getByRole("option", { name: "Run skill" })).toBeVisible();
    await expect(page.getByRole("option", { name: "Judge gate" })).toBeVisible();
    await page.getByRole("option", { name: "Notify member" }).click();
    await expect(page.getByLabel("Member")).toBeVisible();
    await expect(page.getByLabel("Title")).toBeVisible();
    await expect(page.getByLabel("Message")).toBeVisible();
    await capture(page, testInfo, "08-typed-action");
  });

  test("saves a named target and keeps it after reload", async ({ page }) => {
    const slug = "e2e-workspace";
    await page.goto(`/${slug}/workflows/hooks/${HOOK_ID}`);
    await page.getByRole("button", { name: "Configure When" }).click();
    await page.getByRole("combobox", { name: "Scope 1" }).click();
    await page.getByRole("option", { name: "Agent" }).click();
    await page.getByLabel("Agent").click();
    await page.getByRole("button", { name: /Lone/ }).click();
    const saveRequest = page.waitForRequest((request) => request.url().endsWith(`/api/cerebro/workflow-hooks/${HOOK_ID}`) && request.method() === "PUT");
    await page.getByRole("button", { name: "Save draft" }).click();
    expect((await saveRequest).postDataJSON().conditions[0]).toEqual({ field: "issue.status", op: "not_in", values: ["done", "cancelled"] });
    await page.reload();
    await page.getByRole("button", { name: "Configure When" }).click();
    await expect(page.getByLabel("Agent")).toContainText("Lone");
  });

  test("adds an eval gate action and saves its selected eval", async ({ page }) => {
    const slug = "e2e-workspace";
    await page.goto(`/${slug}/workflows/hooks/${HOOK_ID}`);
    await page.getByRole("button", { name: "Configure Actions" }).click();
    await page.getByRole("button", { name: "Add action", exact: true }).click();
    await page.getByLabel("Action 2 type").click();
    await page.getByRole("option", { name: "Eval gate" }).click();
    await page.getByRole("button", { name: "Eval" }).click();
    await page.getByRole("button", { name: /Answer quality/ }).click();

    const saveRequest = page.waitForRequest((request) => request.url().endsWith(`/api/cerebro/workflow-hooks/${HOOK_ID}`) && request.method() === "PUT");
    await page.getByRole("button", { name: "Save draft" }).click();
    const saved = (await saveRequest).postDataJSON();
    expect(saved.handlers[0].actions).toContainEqual({ type: "eval.gate", config: { eval_id: EVAL_ID } });
  });

  test("warns before a live hook is saved as a draft", async ({ page }, testInfo) => {
    const slug = "e2e-workspace";
    await page.goto(`/${slug}/workflows/hooks/${ENFORCED_HOOK_ID}`);
    await expect(page.getByText(/Enforced v2/)).toBeVisible();
    await page.getByRole("button", { name: "Save draft" }).click();
    await expect(page.getByRole("dialog", { name: "Save as draft?" })).toContainText("This hook is live. Saving a draft switches it to Dry run until you publish again.");
    await capture(page, testInfo, "09-enforced-draft-warning");
    await page.getByRole("button", { name: "Keep enforced" }).click();
    await expect(page.getByRole("dialog", { name: "Save as draft?" })).toHaveCount(0);
  });

  test("completes the full Hooks journey at 390 by 844", async ({ page }, testInfo) => {
    const slug = "e2e-workspace";
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto(`/${slug}/workflows`);
    await expect(page.getByRole("button", { name: "Workflow menu" })).toBeVisible();
    await page.getByRole("button", { name: "Workflow menu" }).click();
    await expect(page.getByRole("menuitem", { name: "Hook library" })).toBeVisible();
    await expect(page.getByRole("menuitem", { name: "Workflow log" })).toBeVisible();
    await expectNoHorizontalScroll(page);

    await page.goto(`/${slug}/workflows/hooks`);
    const newHook = page.getByRole("button", { name: "New hook" });
    await expectClickable(page, newHook);
    await expectNoHorizontalScroll(page);
    await capture(page, testInfo, "06-hooks-mobile");
    await newHook.click();

    const recipe = page.getByRole("button", { name: /Require a continuation before completion/ });
    await expectClickable(page, recipe);
    await recipe.click();
    const steps = page.getByRole("list", { name: "Hook steps" });
    await expect(steps.getByRole("button")).toHaveCount(3);

    await page.getByRole("button", { name: "Go to When step" }).click();
    await expect(page.getByRole("region", { name: "when configuration" })).toBeVisible();
    for (const region of ["guide", "actions"]) {
      const next = page.getByRole("button", { name: "Next step" });
      await expectClickable(page, next);
      await next.click();
      await expect(page.getByRole("region", { name: `${region} configuration` })).toBeVisible();
    }
    await page.getByRole("radio", { name: "Stop" }).click();
    const back = page.getByRole("button", { name: "Back one step" });
    await expectClickable(page, back);
    await back.click();
    await expect(page.getByRole("region", { name: "guide configuration" })).toBeVisible();

    await page.getByRole("button", { name: "Go to When step" }).click();
    await page.getByRole("combobox", { name: "Scope 1" }).click();
    await page.getByRole("option", { name: "Agent" }).click();
    await page.getByRole("button", { name: "Agent" }).click();
    const targetSheet = page.locator('[data-slot="drawer-content"]');
    await expect(targetSheet).toBeVisible();
    await expect(targetSheet).toHaveClass(/w-full/);
    await capture(page, testInfo, "07-mobile-target-sheet");
    await targetSheet.getByRole("button", { name: /Lone/ }).click();

    const save = page.getByRole("button", { name: "Save draft" });
    await expectClickable(page, save);
    await save.click();
    await expect(page).toHaveURL(new RegExp(`/workflows/hooks/${NEW_HOOK_ID}$`));

    const testHook = page.getByRole("button", { name: "Test" });
    await expectClickable(page, testHook);
    await testHook.click();
    await expect(page.getByRole("region", { name: "Test and history" })).toBeVisible();

    const publish = page.getByRole("button", { name: "Publish" });
    await expectClickable(page, publish);
    await publish.click();
    const confirm = page.getByRole("button", { name: "Confirm publish" });
    await expectClickable(page, confirm);
    await confirm.click();
    await expect(page.getByText(/Enforced v/)).toBeVisible();
    await expectNoHorizontalScroll(page);
    await capture(page, testInfo, "08-mobile-published-hook");
  });
});

async function expectClickable(page: Page, locator: ReturnType<Page["getByRole"]>) {
  await expect(locator).toBeVisible();
  await expect(locator).toBeEnabled();
  await expect(locator).toBeInViewport();
  await locator.evaluate((element) => {
    const point = element.getBoundingClientRect();
    const top = document.elementFromPoint(point.left + point.width / 2, point.top + point.height / 2);
    if (top !== element && !element.contains(top)) throw new Error("Control is covered and cannot be clicked");
  });
}

async function expectNoHorizontalScroll(page: Page) {
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
}

async function capture(page: Page, testInfo: TestInfo, name: string) {
  const path = `${SHOT_DIR}/${name}.png`;
  await expect(page.locator("body")).not.toHaveCSS("overflow-x", "scroll");
  await page.evaluate(() => document.fonts.ready);
  await page.waitForTimeout(150);
  await page.screenshot({ path, fullPage: false, animations: "disabled" });
  await testInfo.attach(name, { path, contentType: "image/png" });
}

async function mockHookAPI(page: Page) {
  let currentPolicy = { ...basePolicy };
  let enforcedPolicy = { ...basePolicy, id: ENFORCED_HOOK_ID, name: "Enforced policy", mode: "enforce" };
  let newPolicy: typeof basePolicy | null = null;
  await page.route("**/api/agents?**", (route) => route.fulfill({ json: [{ id: "11111111-1111-1111-1111-111111111111", name: "Lone", model: "codex", archived_at: null }] }));
  await page.route("**/api/skills?**", (route) => route.fulfill({ json: [{ id: "44444444-4444-4444-4444-444444444444", name: "verification-before-completion", description: "Verify behavior" }] }));
  await page.route("**/api/cerebro/evals", (route) => route.fulfill({ json: { evals: [{ id: EVAL_ID, title: "Answer quality", status: "active" }] } }));
  await page.route("**/api/cerebro/workflow-hooks**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    if (path.endsWith(`/${NEW_HOOK_ID}/runs`)) return route.fulfill({ json: { runs: [run] } });
    if (path.endsWith(`/${NEW_HOOK_ID}/test`) && request.method() === "POST") {
      if (newPolicy) newPolicy = { ...newPolicy, observed_run_count: 1, can_publish: true };
      return route.fulfill({ json: { side_effects: false, result: run.result, baseline_at: basePolicy.baseline_at } });
    }
    if (path.endsWith(`/${NEW_HOOK_ID}/publish`) && request.method() === "POST") {
      if (newPolicy) newPolicy = { ...newPolicy, mode: "enforce", version: newPolicy.version + 1 };
      return route.fulfill({ json: newPolicy });
    }
    if (path.endsWith(`/${NEW_HOOK_ID}`) && request.method() === "PUT") {
      const body = request.postDataJSON();
      newPolicy = { ...(newPolicy ?? basePolicy), ...body, id: NEW_HOOK_ID, version: (newPolicy?.version ?? 0) + 1 };
      return route.fulfill({ json: newPolicy });
    }
    if (path.endsWith(`/${NEW_HOOK_ID}`)) return route.fulfill({ json: newPolicy });
    if (path.endsWith(`/${HOOK_ID}/runs`)) return route.fulfill({ json: { runs: [run] } });
    if (path.endsWith(`/${HOOK_ID}/test`) && request.method() === "POST") return route.fulfill({ json: { side_effects: false, result: run.result, baseline_at: basePolicy.baseline_at } });
    if (path.endsWith(`/${HOOK_ID}/publish`) && request.method() === "POST") return route.fulfill({ json: { ...currentPolicy, mode: "enforce" } });
    if (path.endsWith(`/${HOOK_ID}`) && request.method() === "PUT") {
      const body = request.postDataJSON();
      currentPolicy = { ...currentPolicy, ...body, id: HOOK_ID, version: currentPolicy.version + 1 };
      return route.fulfill({ json: currentPolicy });
    }
    if (path.endsWith(`/${ENFORCED_HOOK_ID}`) && request.method() === "PUT") {
      const body = request.postDataJSON();
      enforcedPolicy = { ...enforcedPolicy, ...body, id: ENFORCED_HOOK_ID, version: enforcedPolicy.version + 1 };
      return route.fulfill({ json: enforcedPolicy });
    }
    if (path.endsWith(`/${HOOK_ID}`)) return route.fulfill({ json: currentPolicy });
    if (path.endsWith(`/${ENFORCED_HOOK_ID}`)) return route.fulfill({ json: enforcedPolicy });
    if (request.method() === "GET") {
      return route.fulfill({ json: { hooks: [
        { ...basePolicy, id: "00000000-0000-0000-0000-000000000001", name: "Off policy", mode: "off" },
        currentPolicy,
        enforcedPolicy,
        { ...basePolicy, id: "00000000-0000-0000-0000-000000000004", name: "Managed policy", mode: "managed" },
        ...(newPolicy ? [newPolicy] : []),
      ] } });
    }
    if (request.method() === "POST") {
      const body = request.postDataJSON();
      newPolicy = { ...basePolicy, ...body, id: NEW_HOOK_ID, version: 1, observed_run_count: 0, can_publish: false, mode: "dry_run" };
      return route.fulfill({ json: newPolicy });
    }
    return route.fulfill({ status: 200, json: basePolicy });
  });
}
