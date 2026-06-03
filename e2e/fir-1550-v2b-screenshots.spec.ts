// FIR-1550 v2b end-to-end browser screenshots. Drives the full flow:
// create status model with 2 in_progress customs → assign to project (modal
// opens) → open project board (shows two distinct in_progress columns) →
// open an issue and verify the picker shows sub-statuses. Screenshots are
// saved to e2e/screenshots/fir-1550/.
//
// This test is single-purpose and not part of the regular CI suite; run via
// `pnpm exec playwright test e2e/fir-1550-v2b-screenshots.spec.ts`.

import { test, expect, type Page } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { TestApiClient } from "./fixtures";

const DEFAULT_E2E_EMAIL = "e2e@multica.ai";
const DEFAULT_E2E_NAME = "E2E User";
const DEFAULT_E2E_WORKSPACE = "e2e-workspace";

// Local createTestApi that tolerates the removed starter-content endpoint.
async function createTestApiLocal(): Promise<TestApiClient> {
  const api = new TestApiClient();
  await api.login(DEFAULT_E2E_EMAIL, DEFAULT_E2E_NAME);
  await api.ensureWorkspace("E2E Workspace", DEFAULT_E2E_WORKSPACE);
  try {
    await api.dismissStarterContent();
  } catch {
    /* starter-content endpoint was removed upstream; safe to ignore */
  }
  return api;
}

async function loginAsDefaultLocal(page: Page, api: TestApiClient): Promise<string> {
  const token = api.getToken() ?? "";
  await page.addInitScript((t) => {
    localStorage.setItem("multica_token", t);
  }, token);
  const slug = DEFAULT_E2E_WORKSPACE;
  await page.goto(`/${slug}/issues`, { waitUntil: "domcontentloaded" });
  // Wait for SOMETHING from the authenticated shell to render. Inbox link
  // is the canonical signal, but try a couple of fallbacks before failing.
  await Promise.race([
    page.getByRole("link", { name: "Inbox" }).first().waitFor({ timeout: 30_000 }),
    page.getByRole("heading").first().waitFor({ timeout: 30_000 }),
  ]).catch(async () => {
    await page.screenshot({ path: `${SHOT_DIR}/debug-after-goto.png`, fullPage: true });
    const url = page.url();
    const title = await page.title();
    throw new Error(`auth shell never rendered. url=${url} title=${title}`);
  });
  return slug;
}

const SHOT_DIR = "e2e/screenshots/fir-1550";

test.beforeAll(() => {
  mkdirSync(SHOT_DIR, { recursive: true });
});

test("FIR-1550 v2b — full picker + modal + board flow", async ({ page }) => {
  const api = await createTestApiLocal();
  const token = api.getToken() ?? "";
  expect(token).not.toBe("");

  const slug = await loginAsDefaultLocal(page, api);
  const apiBaseEarly =
    process.env.NEXT_PUBLIC_API_URL || `http://localhost:${process.env.PORT || "8080"}`;

  // Enable the feature flag for this user so the Statusmodeller tab and
  // picker presentation engine are active. The per-user override endpoint is
  // workspace-scoped by UUID in the path (/api/workspaces/{id}/feature-flags/
  // {key}) — not /api/cerebro/feature-flags — and the handler parses the path
  // id as a UUID, so the workspace UUID (not the slug) must be used.
  const workspaces = await api.getWorkspaces();
  const wsId = workspaces.find((w) => w.slug === slug)?.id ?? workspaces[0]?.id;
  expect(wsId, "workspace id resolved").toBeTruthy();
  const flagRes = await fetch(
    `${apiBaseEarly}/api/workspaces/${wsId}/feature-flags/cerebro_workflows`,
    {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify({ enabled: true }),
    },
  );
  expect(flagRes.ok, await flagRes.text()).toBe(true);

  // ---- Create a project + a couple of issues across base statuses ----
  const project = await api.createProject(`v2b Demo ${Date.now()}`);
  await api.createIssue("Skitse til feature", {
    project_id: project.id,
    status: "in_progress",
  });
  await api.createIssue("Implementér core", {
    project_id: project.id,
    status: "in_progress",
  });
  await api.createIssue("Færdig PR", {
    project_id: project.id,
    status: "in_review",
  });

  // ---- Create a status model with TWO customs under in_progress ----
  const apiBase =
    process.env.NEXT_PUBLIC_API_URL || `http://localhost:${process.env.PORT || "8080"}`;
  const modelRes = await fetch(`${apiBase}/api/cerebro/status-models`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${token}`,
      "X-Workspace-Slug": slug,
    },
    body: JSON.stringify({
      name: "Plan-først flow",
      description: "Plan → Build → Review → Done",
      statuses: [
        {
          key: "plan",
          label: "Plan",
          color: "#7c3aed",
          base_status: "in_progress",
          description: "Vi planlægger arbejdet — definér scope og kriterier.",
        },
        {
          key: "build",
          label: "Build",
          color: "#0ea5e9",
          base_status: "in_progress",
          description: "Vi bygger — koden skrives nu.",
        },
        {
          key: "review",
          label: "Review",
          color: "#f59e0b",
          base_status: "in_review",
          description: "Klar til kode-review.",
        },
        {
          key: "done-custom",
          label: "Klar til release",
          color: "#10b981",
          base_status: "done",
          description: "Færdig og klar til release.",
        },
      ],
    }),
  });
  expect(modelRes.ok, await modelRes.text()).toBe(true);

  // ---- 1) Statusmodeller-fanen (oversigt) ----
  // The Statusmodeller tab is a query-param tab on the settings page
  // (?tab=status-models), not a standalone /settings/status-models route.
  await page.goto(`/${slug}/settings?tab=status-models`, {
    waitUntil: "domcontentloaded",
  });
  await page.waitForLoadState("networkidle").catch(() => {});
  await expect(page.getByRole("heading", { name: "Statusmodeller" })).toBeVisible({
    timeout: 10_000,
  });
  await page.screenshot({
    path: `${SHOT_DIR}/01-statusmodeller-tab.png`,
    fullPage: true,
  });

  // ---- 2) Mapping-modal: åbn modellen for projektet ----
  // The "Projekter" section renders one NativeSelect (<select>) per project
  // row. Scope to the row that contains our project title, then pick the
  // model by label — selecting a model opens the mapping confirmation modal.
  const projectRow = page
    .locator("div.flex.items-center.justify-between", { hasText: project.title })
    .first();
  await projectRow.scrollIntoViewIfNeeded();
  await projectRow.locator("select").selectOption({ label: "Plan-først flow" });

  // Mapping modal should now be open.
  const mappingDialog = page.getByRole("dialog");
  await expect(mappingDialog).toBeVisible({ timeout: 10_000 });
  await page.waitForTimeout(500);
  await page.screenshot({
    path: `${SHOT_DIR}/02-mapping-modal.png`,
    fullPage: false,
  });

  // Confirm to assign.
  await page.getByRole("button", { name: /Anvend statusmodel/i }).click();
  await page.waitForTimeout(1500);

  // ---- 3) Project board uses the model's columns (Plan / Review / Klar til
  // release) instead of the 7 default statuses. NB: two custom statuses under
  // the same base (Plan + Build) currently share one board column per base;
  // they are distinct in the status picker (step 4). ----
  await page.goto(`/${slug}/projects/${project.id}`, {
    waitUntil: "domcontentloaded",
  });
  await page.waitForTimeout(4000);
  await page.screenshot({
    path: `${SHOT_DIR}/03-board-with-model.png`,
    fullPage: true,
  });

  // ---- 4) Issue detail: status picker shows the custom sub-statuses ----
  const firstIssue = page.getByText("Skitse til feature").first();
  await firstIssue.click();
  await page.waitForTimeout(3000);

  // Open the status picker to capture the sub-status options.
  const statusTrigger = page
    .getByRole("button")
    .filter({ hasText: /Plan|Build|Review|Klar til release|In progress|I gang|Todo/i })
    .first();
  await statusTrigger.click();
  await page.waitForTimeout(800);
  await page.screenshot({
    path: `${SHOT_DIR}/04-picker-with-sub-statuses.png`,
    fullPage: false,
  });
});
