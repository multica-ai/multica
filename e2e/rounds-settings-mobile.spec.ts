/**
 * Mobile regression for the Rounds "Manage rounds" panel (FIR-3107).
 *
 * On a phone-sized viewport the panel renders as a bottom drawer. The
 * create/edit form (name, type, schedule, time, timezone, footer) must fit
 * within the visible viewport or be scrollable to it — the Save button has
 * to be reachable.
 */
import { test, expect } from "@playwright/test";
import pg from "pg";
import { createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

let api: TestApiClient;
let roundIds: string[] = [];

test.beforeEach(async () => {
  api = await createTestApi();
  await api.setWorkspaceFeatureFlag("cerebro_inbox_rounds", true);
  // Enough rounds that the manage list view is taller than the drawer.
  roundIds = [];
  for (let i = 1; i <= 6; i++) {
    const res = await (api as any).authedFetch("/api/cerebro/rounds", {
      method: "POST",
      body: JSON.stringify({ name: `Round ${i}`, mode: "batch", schedule_cron: null, timezone: "UTC" }),
    });
    if (res.ok) roundIds.push((await res.json()).id);
  }
});

test.afterEach(async () => {
  for (const id of roundIds) {
    await (api as any).authedFetch(`/api/cerebro/rounds/${id}`, { method: "DELETE" }).catch(() => {});
  }
  await api.cleanup();
});

// iPhone 13/14 *visible* viewport: 390x844 screen minus Safari chrome
// (status bar + URL bar + bottom toolbar ≈ 180px). Layout bugs hide in the
// difference between the full screen and what the browser actually shows.
test.use({ viewport: { width: 390, height: 664 } });

async function loginInBrowser(page: import("@playwright/test").Page, email: string) {
  const dbUrl = process.env.DATABASE_URL ?? "";
  const client = new pg.Client(dbUrl);
  await client.connect();
  let token = "";
  try {
    await client.query("DELETE FROM verification_code WHERE email = $1", [email]);
    await page.goto("/login", { waitUntil: "domcontentloaded" });
    const sent = await page.evaluate(async (e) => {
      const r = await fetch("/auth/send-code", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email: e }),
      });
      return r.ok;
    }, email);
    if (!sent) throw new Error("send-code failed");
    const codeRes = await client.query(
      "SELECT code FROM verification_code WHERE email = $1 AND used = FALSE ORDER BY created_at DESC LIMIT 1",
      [email],
    );
    const code = codeRes.rows[0].code;
    const verifyOut = await page.evaluate(async (args) => {
      const r = await fetch("/auth/verify-code", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email: args.email, code: args.code }),
      });
      const json = r.ok ? await r.json() : null;
      return { ok: r.ok, token: json?.token ?? "" };
    }, { email, code });
    if (!verifyOut.ok) throw new Error("verify-code failed");
    token = verifyOut.token;
    await client.query("DELETE FROM verification_code WHERE email = $1", [email]);
  } finally {
    await client.end();
  }
  await page.evaluate((t) => localStorage.setItem("multica_token", t), token);
}

test("Manage rounds create form is fully reachable on mobile", async ({ page }) => {
  const slug = (api as any).workspaceSlug as string;
  await loginInBrowser(page, "e2e@multica.ai");

  await page.goto(`/${slug}/inbox`, { waitUntil: "networkidle" });

  // Add the Rounds section via the inbox ⋯ menu (the layout persists across
  // runs, so only add it when no Rounds block is present yet).
  const manageButton = page.getByRole("button", { name: "Manage rounds" }).first();
  if (!(await manageButton.isVisible().catch(() => false))) {
    await page.locator('button[title="Inbox menu"]').click();
    await page.getByRole("menuitem", { name: "Rounds", exact: true }).click();
  }

  // Open the Manage rounds panel from the Rounds block header.
  await manageButton.click();
  await expect(page.getByRole("button", { name: "Create round" })).toBeVisible();
  await page.screenshot({ path: "screenshots/rounds-manage-mobile.png" });

  const viewport = page.viewportSize()!;

  // The manage list view must keep every round reachable: the last round's
  // Edit button must be scrollable into the viewport.
  const lastEdit = page.getByRole("button", { name: "Edit Round 6" });
  await lastEdit.scrollIntoViewIfNeeded();
  await expect(lastEdit).toBeVisible();
  const lastEditBox = await lastEdit.boundingBox();
  expect(lastEditBox).not.toBeNull();
  expect(lastEditBox!.y + lastEditBox!.height).toBeLessThanOrEqual(viewport.height + 1);
  await page.screenshot({ path: "screenshots/rounds-manage-mobile-bottom.png" });

  // Switch to the create form (the tall state).
  await page.getByRole("button", { name: "Create round" }).scrollIntoViewIfNeeded();
  await page.getByRole("button", { name: "Create round" }).click();
  await expect(page.getByLabel("Round name")).toBeVisible();
  await page.screenshot({ path: "screenshots/rounds-create-mobile.png" });
  // Every form control must be reachable: scroll it into view and assert it
  // lands fully inside the viewport.
  for (const label of ["Round name", "Timezone"]) {
    const el = page.getByLabel(label);
    await el.scrollIntoViewIfNeeded();
    await expect(el).toBeVisible();
    const box = await el.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.y).toBeGreaterThanOrEqual(0);
    expect(box!.y + box!.height).toBeLessThanOrEqual(viewport.height + 1);
  }
  // Save must be on screen WITHOUT scrolling (sticky footer) — on a phone the
  // keyboard hides anything below the fold, so a Save that has to be scrolled
  // to is effectively unreachable while typing.
  const save = page.getByRole("button", { name: "Save round" });
  await expect(save).toBeVisible();
  const saveBox = await save.boundingBox();
  expect(saveBox).not.toBeNull();
  expect(saveBox!.y).toBeGreaterThanOrEqual(0);
  expect(saveBox!.y + saveBox!.height).toBeLessThanOrEqual(viewport.height + 1);
  await page.screenshot({ path: "screenshots/rounds-create-mobile-bottom.png" });

  // Shrink to roughly the visible area left when the iOS keyboard is open.
  // The form must stay usable: Save still on screen without scrolling, and
  // every field still reachable by scrolling.
  await page.setViewportSize({ width: 390, height: 400 });
  await expect(save).toBeVisible();
  const smallSave = await save.boundingBox();
  expect(smallSave).not.toBeNull();
  expect(smallSave!.y + smallSave!.height).toBeLessThanOrEqual(401);
  for (const label of ["Round name", "Timezone"]) {
    const el = page.getByLabel(label);
    await el.scrollIntoViewIfNeeded();
    await expect(el).toBeVisible();
  }
  await page.screenshot({ path: "screenshots/rounds-create-mobile-keyboard.png" });
});
