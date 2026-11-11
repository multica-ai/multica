import { test, expect } from "@playwright/test";
import pg from "pg";
import { TestApiClient } from "./fixtures";

test("delete a failed inbox entry with confirmation, retry, and cross-session refresh", async ({ page, context }, testInfo) => {
  test.setTimeout(120000);
  const consoleErrors: string[] = [];
  page.on("console", (message) => { if (message.type() === "error") consoleErrors.push(message.text()); });
  const api = new TestApiClient();
  const run = `${Date.now()}-${process.pid}`;
  const login = await api.login(`inbox-delete-${run}@localhost`, "Inbox deletion test");
  const workspace = await api.ensureWorkspace("Inbox deletion", `inbox-delete-${run}`);
  await api.markUserOnboarded();
  const db = new pg.Client(process.env.DATABASE_URL);
  await db.connect();
  try {
    const seeded = await db.query(
      `INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id, type, title, body)
       VALUES ($1, 'member', $2, 'quick_create_failed', 'Failed test run', 'This failed test notification is no longer needed.'),
              ($1, 'member', $2, 'system', 'Keep this notification', 'Other notifications are not deleted.')
       RETURNING id`,
      [workspace.id, login.user.id],
    );
    const id = seeded.rows[0].id;
    await context.addInitScript((token) => {
      localStorage.setItem("multica_token", token!);
      localStorage.setItem("multica:chat:isOpen", "false");
    }, api.getToken());
    await page.setViewportSize({ width: 1280, height: 850 });
    await page.goto(`/${workspace.slug}/inbox`, { waitUntil: "domcontentloaded" });
    const row = page.locator('[data-mc="inbox-item"]').filter({ hasText: "Failed test run" });
    await expect(row).toBeVisible({ timeout: 60000 });
    const other = await context.newPage();
    await other.goto(`/${workspace.slug}/inbox`);
    const otherRow = other.locator('[data-mc="inbox-item"]').filter({ hasText: "Failed test run" });
    await expect(otherRow).toBeVisible();

    await row.click();
    await row.click({ button: "right" });
    await expect(page.getByRole("menuitem", { name: "Delete", exact: true })).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath("inbox-delete-menu.png") });
    await page.getByRole("menuitem", { name: "Delete", exact: true }).click();
    const dialog = page.getByRole("alertdialog", { name: "Delete notification?" });
    await expect(dialog).toBeVisible();
    await expect(dialog).toHaveCSS("opacity", "1");
    await expect(page.getByRole("menuitem", { name: "Delete", exact: true })).not.toBeVisible();
    await page.screenshot({ path: testInfo.outputPath("inbox-delete-confirm.png") });
    await dialog.getByRole("button", { name: "Cancel" }).click();
    await expect(row).toBeVisible();
    expect((await db.query("SELECT id FROM inbox_item WHERE id = $1", [id])).rowCount).toBe(1);

    await row.click({ button: "right" });
    await page.getByRole("menuitem", { name: "Delete", exact: true }).click();
    const route = `**/api/inbox/${id}`;
    await page.route(route, async (request) => {
      if (request.request().method() === "DELETE") {
        await request.fulfill({ status: 500, contentType: "application/json", body: JSON.stringify({ error: "Test failure" }) });
      } else await request.continue();
    });
    await dialog.getByRole("button", { name: "Delete", exact: true }).click();
    await expect(dialog.getByRole("alert")).toContainText("Failed to delete notification");
    await expect(row).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath("inbox-delete-retry.png") });
    await page.unroute(route);
    await dialog.getByRole("button", { name: "Delete", exact: true }).click();
    await expect(dialog).not.toBeVisible();
    await expect(row).toHaveCount(0);
    await expect(otherRow).toHaveCount(0);
    await expect(page.locator('[data-mc="inbox-item"]').filter({ hasText: "Keep this notification" })).toBeVisible();
    expect((await db.query("SELECT id FROM inbox_item WHERE id = $1", [id])).rowCount).toBe(0);
    await page.screenshot({ path: testInfo.outputPath("inbox-delete-after.png") });
    await page.reload();
    await expect(row).toHaveCount(0);
    await other.close();
  } finally {
    await testInfo.attach("console-errors", { body: consoleErrors.join("\n"), contentType: "text/plain" });
    await db.end();
    await api.deleteWorkspace();
  }
});
