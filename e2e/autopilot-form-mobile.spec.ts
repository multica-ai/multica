/**
 * Mobile regression for the autopilot create/edit form (JEH-1897).
 *
 * The shared form lives in @multica/cerebro-autopilot-pages and is mounted
 * by /autopilots/new and /autopilots/:id/edit. On mobile, the body container
 * used to clip the title editor behind the sticky page header because the
 * right config aside was `shrink-0` with enough content to squash the left
 * column (which holds the title) to 0px. The fix flips the mobile body to
 * page-level scroll while keeping the desktop two-pane scrolling intact.
 */
import { test, expect } from "@playwright/test";
import pg from "pg";
import { createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

let api: TestApiClient;

test.beforeEach(async () => {
  api = await createTestApi();
});

test.afterEach(async () => {
  await api.cleanup();
});

// iPhone 13/14 viewport.
test.use({ viewport: { width: 390, height: 844 } });

/**
 * Drive the production verify-code flow inside the browser context so the
 * HttpOnly auth cookie is set on the frontend origin (TestApiClient.login
 * runs outside the browser so its cookies do not reach the page).
 */
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

test("title editor sits below the sticky header on mobile", async ({ page }) => {
  const slug = (api as any).workspaceSlug as string;
  await loginInBrowser(page, "e2e@multica.ai");

  await page.goto(`/${slug}/autopilots/new`, { waitUntil: "networkidle" });

  const title = page.locator(".title-editor").first();
  await expect(title).toBeVisible({ timeout: 30000 });

  // PageHeader is h-12 (48px), sticky top-0. The title must clear it.
  const titleBox = await title.boundingBox();
  expect(titleBox).not.toBeNull();
  expect(titleBox!.y).toBeGreaterThanOrEqual(48);

  // The user must be able to focus the title — if it were behind an overlay
  // (the original bug), the click would fall through to the covering element.
  await title.click();
  const focused = await page.evaluate(() => {
    const el = document.querySelector(".title-editor");
    return !!el && el.contains(document.activeElement);
  });
  expect(focused).toBeTruthy();

  // Every right-pane section must be reachable; the page should scroll on
  // mobile rather than clipping the aside.
  for (const sectionText of ["Output mode", "Model override", "Privacy", "Schedule"]) {
    const el = page.getByText(sectionText, { exact: false }).first();
    await el.scrollIntoViewIfNeeded();
    await expect(el).toBeVisible();
  }
});
