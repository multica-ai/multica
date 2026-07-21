/**
 * Mobile + builder regression for the Evals catalog (FIR-3496 design review).
 *
 * Three things this guards, all of which were broken or unproven before:
 *  1. The create form leads with the eval's content. It used to open with nine
 *     required identity/source fields — including a code-repo file path — before
 *     asking what the eval tests. Key and source path are now derived from the
 *     title and live under Advanced.
 *  2. At phone width the catalog renders as cards, so Edit/Duplicate/Delete stay
 *     on screen. The desktop table put them in a sixth column reachable only by
 *     scrolling sideways.
 *  3. The detail header wraps instead of pushing its action off the viewport.
 *
 * The API is mocked (same approach as cerebro-evals-v2.spec.ts) so the run needs
 * only the frontend, and asserts on layout rather than on stored rows.
 */
import { expect, test, type Page } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";

const EVAL_ID = "66666666-6666-6666-6666-666666666666";
const WORKFLOW_ID = "77777777-7777-7777-7777-777777777777";

const evalItem = {
  id: EVAL_ID, workspace_id: "workspace", key: "answer-quality", version: "1.0.0", title: "Answer quality",
  description: "", status: "active", owner: {}, objective: "Answer correctly",
  target: { kind: "agent", locator: "mention://agent/lone", ref: "main" },
  datasets: [{ id: "t1", situation: "Angry refund request", expected: "offer refund", critical: true }],
  graders: [{ id: "grader-1", type: "ai_judge", config: {} }],
  thresholds: [{ metric: "pass_rate", operator: "gte", value: 0.8 }, { metric: "all_critical_pass", operator: "eq", value: true }],
  runner: {}, source: { repository: "https://github.com/firtal-group/firtal-evals", path: "evals/answer-quality/eval.json" },
  created_by_id: "member", created_by_type: "member",
  created_at: "2026-07-19T08:00:00Z", updated_at: "2026-07-19T08:00:00Z",
};

const workflow = {
  id: WORKFLOW_ID, workspace_id: "workspace", name: "Support loop", enabled: true, workflow_type: "issue_loop",
  trigger_type: "status_changed", trigger_config: {}, conditions: [], action_type: "set_status", action_config: {}, editor_mode: "form",
  created_by_id: "member", created_by_type: "member", created_at: "2026-07-19T08:00:00Z", updated_at: "2026-07-19T08:00:00Z",
};

const PHONE = { width: 390, height: 844 };

test.describe("FIR-3496 Evals catalog — builder and phone layout", () => {
  test.beforeEach(async ({ page }) => {
    const api = await createTestApi();
    await api.setWorkspaceFeatureFlag("cerebro_workflows", true);
    await api.setWorkspaceFeatureFlag("cerebro_evals", true);
    await mockEvalAPI(page);
    await loginAsDefault(page);
  });

  test("the create form asks for the eval's content before any identity field", async ({ page }) => {
    await page.goto("/e2e-workspace/workflows/evals");
    await page.getByRole("button", { name: "New eval" }).click();

    const form = page.locator("form");
    await expect(form.getByLabel("Title")).toBeVisible();

    // Key, Version and every Source field start hidden behind Advanced.
    await expect(form.getByLabel("Key")).toBeHidden();
    await expect(form.getByLabel("Source path")).toBeHidden();
    await expect(form.getByLabel("Source repository")).toBeHidden();

    // Title comes before the target picker in the document, so the first thing
    // asked for is what the eval is about.
    const order = await form.evaluate((el) => {
      const labels = Array.from(el.querySelectorAll("label")).map((l) => l.textContent?.trim() ?? "");
      return { title: labels.findIndex((t) => t.startsWith("Title")), objective: labels.findIndex((t) => t.startsWith("Objective")) };
    });
    expect(order.title).toBeGreaterThanOrEqual(0);
    expect(order.title).toBeLessThan(order.objective);
  });

  test("key and source path follow the title, and Advanced reveals them", async ({ page }) => {
    await page.goto("/e2e-workspace/workflows/evals");
    await page.getByRole("button", { name: "New eval" }).click();

    const form = page.locator("form");
    await form.getByLabel("Title").fill("Refund tone check");
    await form.getByRole("button", { name: /Advanced/ }).click();

    await expect(form.getByLabel("Key")).toHaveValue("refund-tone-check");
    await expect(form.getByLabel("Source path")).toHaveValue("evals/refund-tone-check/eval.json");
  });

  test("target type is a closed list, not free text", async ({ page }) => {
    await page.goto("/e2e-workspace/workflows/evals");
    await page.getByRole("button", { name: "New eval" }).click();

    const target = page.locator("form").getByLabel("What is being tested");
    await expect(target).toHaveJSProperty("tagName", "SELECT");
    // A typo used to be saveable here and only failed later, on Run now.
    await target.selectOption("workflow");
    await expect(target).toHaveValue("workflow");
  });

  test.describe("at phone width", () => {
    test.use({ viewport: PHONE });

    test("catalog row actions stay on screen instead of behind a sideways scroll", async ({ page }) => {
      await page.goto("/e2e-workspace/workflows/evals");

      const card = page.locator('article:has-text("Answer quality")');
      await expect(card).toBeVisible();

      // The desktop table must not be the thing rendering at this width.
      await expect(page.getByRole("table")).toBeHidden();

      for (const name of ["Runs", "Edit", "Duplicate", "Delete"]) {
        const action = card.getByRole("button", { name });
        await expect(action).toBeVisible();
        const box = await action.boundingBox();
        expect(box, `${name} has no box`).not.toBeNull();
        // Fully inside the viewport — the actual regression was actions sitting
        // past the right edge of a horizontally scrolling table.
        expect(box!.x).toBeGreaterThanOrEqual(0);
        expect(box!.x + box!.width).toBeLessThanOrEqual(PHONE.width);
      }
    });

    test("the detail header keeps Run now inside the viewport", async ({ page }) => {
      await page.goto("/e2e-workspace/workflows/evals");
      await page.locator('article:has-text("Answer quality")').getByText("Answer quality").click();

      const runNow = page.getByRole("button", { name: "Run now" });
      await expect(runNow).toBeVisible();
      const box = await runNow.boundingBox();
      expect(box).not.toBeNull();
      expect(box!.x).toBeGreaterThanOrEqual(0);
      expect(box!.x + box!.width).toBeLessThanOrEqual(PHONE.width);
    });

    test("every detail tab is reachable", async ({ page }) => {
      await page.goto("/e2e-workspace/workflows/evals");
      await page.locator('article:has-text("Answer quality")').getByText("Answer quality").click();

      for (const name of [/^Overview$/, /^Tasks/, /^Runs$/, /^Connections/, /^Settings$/]) {
        const tab = page.getByRole("tab", { name });
        await tab.scrollIntoViewIfNeeded();
        await expect(tab).toBeVisible();
      }
    });

    test("deleting asks in an in-app dialog, not a native confirm", async ({ page }) => {
      await page.goto("/e2e-workspace/workflows/evals");

      // If the page still called window.confirm() this listener would fire and
      // the assertion below would never find a rendered dialog.
      let nativeDialog = false;
      page.on("dialog", (d) => { nativeDialog = true; void d.dismiss(); });

      await page.locator('article:has-text("Answer quality")').getByRole("button", { name: "Delete" }).click();

      const dialog = page.getByRole("alertdialog");
      await expect(dialog).toBeVisible();
      await expect(dialog.getByText("Delete eval?")).toBeVisible();
      expect(nativeDialog).toBe(false);
    });
  });
});

async function mockEvalAPI(page: Page) {
  await page.route("**/api/cerebro/workflows", (route) => route.fulfill({ json: { workflows: [workflow] } }));
  await page.route("**/api/cerebro/evals**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.endsWith("/bindings")) return route.fulfill({ json: { bindings: [] } });
    if (path.endsWith("/runs")) return route.fulfill({ json: { runs: [] } });
    if (path.endsWith("/schedule")) return route.fulfill({ json: null });
    return route.fulfill({ json: { evals: [evalItem] } });
  });
}
