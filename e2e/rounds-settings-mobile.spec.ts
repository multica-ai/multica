/**
 * Regression for the Rounds "Manage rounds" panel (FIR-3107, FIR-3293).
 *
 * FIR-3293: the panel is laid out inside containers whose track is sized by
 * their content's min-content. An issue title is white-space:nowrap
 * (truncate), so its min-content is the entire string — the panel grew wider
 * than the dialog/drawer and pushed Delete and the per-issue remove buttons
 * outside it, instead of truncating. The guard is therefore: the panel must
 * never scroll horizontally, and every action must sit inside it.
 *
 * The previous version of this spec waited on a "Timezone" field that stopped
 * existing when rounds were simplified to name-only (FIR-3179), so it timed
 * out instead of guarding anything. Keep these assertions tied to controls the
 * panel actually renders.
 */
import { test, expect, type Page } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

let api: TestApiClient;
let slug = "";
let roundId = "";

// A title long enough that its min-content is wider than the panel.
const LONG_TITLE =
  "Agent Office — versioneringssystem for al agent-kontekst (scope + review)";
const ROUND = "Overflow Guard";

/** Rounds outlive api.cleanup(), so clear them or stale members leak in. */
async function deleteAllRounds(client: TestApiClient) {
  const res = await (client as any).authedFetch("/api/cerebro/rounds");
  if (!res.ok) return;
  const { rounds = [] } = await res.json();
  for (const r of rounds) {
    await (client as any).authedFetch(`/api/cerebro/rounds/${r.id}`, { method: "DELETE" }).catch(() => {});
  }
}

test.beforeEach(async ({ page }) => {
  api = await createTestApi();
  await api.setWorkspaceFeatureFlag("cerebro_inbox_rounds", true);
  slug = await loginAsDefault(page);
  await deleteAllRounds(api);

  const res = await (api as any).authedFetch("/api/cerebro/rounds", {
    method: "POST",
    body: JSON.stringify({ name: ROUND }),
  });
  if (!res.ok) throw new Error(`create round failed: ${res.status} ${await res.text()}`);
  roundId = (await res.json()).id;
  // Every member is long-titled, so any of them must truncate.
  // allow_duplicate: the same titles are re-seeded on every run, and the API
  // rejects an active duplicate title — without this the issue never gets
  // created and the round silently renders with no members.
  for (let i = 0; i < 6; i++) {
    const issue = await api.createIssue(`${LONG_TITLE} ${i}`, { allow_duplicate: true });
    if (!issue?.id) throw new Error(`create issue failed: ${JSON.stringify(issue)}`);
    const add = await (api as any).authedFetch(`/api/cerebro/rounds/${roundId}/members`, {
      method: "POST",
      body: JSON.stringify({ issue_id: issue.id }),
    });
    if (!add.ok) throw new Error(`add member failed: ${add.status} ${await add.text()}`);
  }
});

test.afterEach(async () => {
  // beforeEach can fail before api exists; don't mask that error with a TypeError.
  if (!api) return;
  await deleteAllRounds(api);
  await api.cleanup();
});

// iPhone 13/14 *visible* viewport: 390x844 screen minus Safari chrome
// (status bar + URL bar + bottom toolbar ~= 180px). Layout bugs hide in the
// difference between the full screen and what the browser actually shows.
test.use({ viewport: { width: 390, height: 664 } });

async function openManageRounds(page: Page) {
  await page.goto(`/${slug}/inbox`, { waitUntil: "networkidle" });
  const manage = page.getByRole("button", { name: "Manage rounds" }).first();
  if (!(await manage.isVisible().catch(() => false))) {
    await page.locator('button[title="Inbox menu"]').click();
    await page.getByRole("menuitem", { name: "Rounds", exact: true }).click();
  }
  await manage.click();
  await expect(page.getByRole("button", { name: "Create round" })).toBeVisible();
  // Let the open animation settle before measuring.
  await page.waitForTimeout(500);
}

/** The panel must never need horizontal scrolling to reach its controls. */
async function expectNoHorizontalOverflow(page: Page, selector: string) {
  const overflow = await page
    .locator(selector)
    .evaluate((el) => el.scrollWidth - (el as HTMLElement).clientWidth);
  expect(overflow, `${selector} scrolls horizontally by ${overflow}px`).toBeLessThanOrEqual(1);
}

/** Every control must render inside the panel's own box. */
async function expectInsidePanel(
  page: Page,
  panel: string,
  buttonName: string | RegExp,
  label = String(buttonName),
) {
  const panelBox = (await page.locator(panel).boundingBox())!;
  const btn = page.getByRole("button", { name: buttonName }).first();
  await expect(btn).toBeVisible();
  const box = (await btn.boundingBox())!;
  expect(
    box.x + box.width,
    `"${label}" right edge is outside the panel`,
  ).toBeLessThanOrEqual(panelBox.x + panelBox.width + 1);
  expect(box.x, `"${label}" left edge is outside the panel`).toBeGreaterThanOrEqual(panelBox.x - 1);
}

// Member rows are labelled "Remove <identifier> · <title>", so match the verb.
const REMOVE_MEMBER = /^Remove .+/;

test("Manage rounds panel keeps every action reachable on mobile", async ({ page }) => {
  await openManageRounds(page);
  const drawer = '[data-slot="drawer-content"]';

  await expectNoHorizontalOverflow(page, drawer);
  await expectInsidePanel(page, drawer, `Edit ${ROUND}`);
  await expectInsidePanel(page, drawer, `Delete ${ROUND}`);

  // The drawer must stay within the visible viewport.
  const box = (await page.locator(drawer).boundingBox())!;
  expect(box.y + box.height).toBeLessThanOrEqual(664 + 1);
  await page.screenshot({ path: "screenshots/rounds-manage-mobile.png" });

  // Expanding a round must not make the panel overflow sideways: long titles
  // truncate instead (FIR-3293).
  await page.getByRole("button", { name: `Expand ${ROUND}` }).click();
  await page.waitForTimeout(300);
  await expectNoHorizontalOverflow(page, drawer);
  const title = page.locator(`${drawer} span.truncate`).last();
  const truncated = await title.evaluate((el) => el.scrollWidth > (el as HTMLElement).offsetWidth);
  expect(truncated, "long issue title should be truncated, not overflowing").toBe(true);
  await expectInsidePanel(page, drawer, REMOVE_MEMBER, "Remove issue");
  await page.screenshot({ path: "screenshots/rounds-manage-mobile-expanded.png" });

  // Save must be on screen WITHOUT scrolling (sticky footer) — on a phone the
  // keyboard hides anything below the fold, so a Save that has to be scrolled
  // to is effectively unreachable while typing.
  await page.getByRole("button", { name: "Create round" }).click();
  await expect(page.getByLabel("Round name")).toBeVisible();
  const save = page.getByRole("button", { name: "Save round" });
  await expect(save).toBeVisible();
  const saveBox = (await save.boundingBox())!;
  expect(saveBox.y).toBeGreaterThanOrEqual(0);
  expect(saveBox.y + saveBox.height).toBeLessThanOrEqual(664 + 1);
  await page.screenshot({ path: "screenshots/rounds-create-mobile.png" });
});

test("Manage rounds panel keeps every action reachable on desktop", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await openManageRounds(page);
  const dialog = '[data-slot="dialog-content"]';

  await expectNoHorizontalOverflow(page, dialog);
  await expectInsidePanel(page, dialog, `Edit ${ROUND}`);
  await expectInsidePanel(page, dialog, `Delete ${ROUND}`);

  // Collapsed by default: the panel must not run off the bottom of the screen
  // just because a round has many members (FIR-3293).
  const box = (await page.locator(dialog).boundingBox())!;
  expect(box.y).toBeGreaterThanOrEqual(0);
  expect(box.y + box.height).toBeLessThanOrEqual(900 + 1);
  await page.screenshot({ path: "screenshots/rounds-manage-desktop.png" });

  await page.getByRole("button", { name: `Expand ${ROUND}` }).click();
  await page.waitForTimeout(300);
  await expectNoHorizontalOverflow(page, dialog);
  await expectInsidePanel(page, dialog, REMOVE_MEMBER, "Remove issue");
  await page.screenshot({ path: "screenshots/rounds-manage-desktop-expanded.png" });
});
