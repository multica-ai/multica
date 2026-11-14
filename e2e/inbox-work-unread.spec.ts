import { test, expect } from "@playwright/test";
import pg from "pg";
import { TestApiClient } from "./fixtures";

test("marking Inbox all read clears Work Unread in another open session", async ({ page, context }, testInfo) => {
  test.setTimeout(120000);
  const api = new TestApiClient();
  const run = `${Date.now()}-${process.pid}`;
  const login = await api.login(`inbox-work-${run}@localhost`, "Unread sync test");
  const workspace = await api.ensureWorkspace("Unread sync", `unread-sync-${run}`);
  await api.markUserOnboarded();
  const issue = await api.createIssue("Review the completed agent result");
  const db = new pg.Client(process.env.DATABASE_URL);
  await db.connect();
  try {
    await db.query(
      `INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id, issue_id, type, title)
       VALUES ($1, 'member', $2, $3, 'new_comment', $4)`,
      [workspace.id, login.user.id, issue.id, issue.title],
    );
    await db.query(
      `INSERT INTO issue_unread (workspace_id, user_id, issue_id, outcome)
       VALUES ($1, $2, $3, 'completed')`,
      [workspace.id, login.user.id, issue.id],
    );
    await context.addInitScript((token) => {
      localStorage.setItem("multica_token", token!);
      localStorage.setItem("multica:chat:isOpen", "false");
    }, api.getToken());
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto(`/${workspace.slug}/inbox`, { waitUntil: "domcontentloaded" });
    await expect(page.locator('[data-mc="inbox-item"]').filter({ hasText: issue.title })).toBeVisible({ timeout: 60000 });
    const work = await context.newPage();
    await work.setViewportSize({ width: 1440, height: 900 });
    await work.goto(`/${workspace.slug}/issues?view=unread`, { waitUntil: "domcontentloaded" });
    await expect(work.getByText(issue.title, { exact: true }).first()).toBeVisible({ timeout: 60000 });
    await work.screenshot({ path: testInfo.outputPath("work-unread-before-read-all.png"), animations: "disabled" });

    await page.getByRole("button", { name: "More actions", exact: true }).click();
    await page.getByRole("menuitem", { name: "Mark all as read", exact: true }).click();
    await expect(work.getByText(issue.title, { exact: true })).toHaveCount(0, { timeout: 15000 });
    await expect(work.getByText("No unread issues", { exact: true })).toBeVisible();
    expect((await db.query("SELECT count(*) FROM issue_unread WHERE workspace_id = $1 AND user_id = $2", [workspace.id, login.user.id])).rows[0].count).toBe("0");
    expect((await db.query("SELECT count(*) FROM inbox_item WHERE workspace_id = $1 AND recipient_id = $2 AND archived = false AND read = false", [workspace.id, login.user.id])).rows[0].count).toBe("0");
    await work.screenshot({ path: testInfo.outputPath("work-unread-after-read-all.png"), animations: "disabled" });
    await page.screenshot({ path: testInfo.outputPath("inbox-after-read-all.png"), animations: "disabled" });
    await work.reload();
    await expect(work.getByText(issue.title, { exact: true })).toHaveCount(0);
    await expect(work.getByText("No unread issues", { exact: true })).toBeVisible();
    await work.close();
  } finally {
    await db.end();
    await api.cleanup();
    await api.deleteWorkspace();
  }
});
