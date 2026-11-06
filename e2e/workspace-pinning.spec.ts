import { test, expect } from "@playwright/test";
import pg from "pg";
import { TestApiClient } from "./fixtures";

test("workspace unread stays visible before pinning and after unpinning", async ({ page }, testInfo) => {
  const viewer = new TestApiClient();
  const owner = new TestApiClient();
  const run = `${Date.now()}-${process.pid}`;
  const viewerEmail = `workspace-pin-viewer-${run}@localhost`;
  const login = await viewer.login(viewerEmail, "Workspace pin viewer");
  const home = await viewer.ensureWorkspace("Pin home", `pin-home-${run}`);
  await viewer.markUserOnboarded();
  await owner.login(`workspace-pin-owner-${run}@localhost`, "Workspace pin owner");
  const target = await owner.ensureWorkspace("Pin target", `pin-target-${run}`);
  try {
    await owner.addMemberByEmail(viewerEmail);
    const db = new pg.Client(process.env.DATABASE_URL);
    await db.connect();
    try {
      await db.query(
        `INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id, type, title)
         VALUES ($1, 'member', $2, 'mention', 'Workspace unread regression')`,
        [target.id, login.user.id],
      );
    } finally {
      await db.end();
    }
    await page.addInitScript((token) => {
      localStorage.setItem("multica_token", token!);
      localStorage.setItem("multica:chat:isOpen", "false");
    }, viewer.getToken());
    await page.setViewportSize({ width: 1280, height: 820 });
    await page.goto(`/${home.slug}/issues`, { waitUntil: "domcontentloaded" });
    const trigger = page.getByRole("button", { name: "Switch workspace" });
    await expect(trigger).toBeVisible({ timeout: 30000 });
    await trigger.click();
    const menu = page.getByRole("menu");
    const row = menu.getByRole("menuitem", { name: /Pin target/ });
    const dot = row.locator(".bg-brand");
    // A DOM-only assertion misses the inline span collapsing to zero size.
    await expect(dot).toBeVisible();
    const setPin = async (label: string) => {
      await row.hover();
      const response = page.waitForResponse((response) =>
        response.url().endsWith(`/api/workspaces/${target.id}/pin`)
          && response.request().method() === "PUT",
      );
      await row.getByRole("button", { name: label, exact: true }).click();
      expect((await response).status()).toBe(200);
      await expect(menu).toBeVisible();
      await expect(page).toHaveURL(new RegExp(`/${home.slug}/issues$`));
    };
    await setPin("Pin workspace");
    await page.mouse.move(600, 450);
    await expect(row.locator(".icon-pin.fill-current.text-brand")).toBeVisible();
    await setPin("Unpin workspace");
    await page.mouse.move(600, 450);
    await expect(dot).toBeVisible();
    await page.keyboard.press("Escape");
    await page.reload({ waitUntil: "domcontentloaded" });
    await trigger.click();
    await expect(dot).toBeVisible();
    await page.screenshot({
      path: testInfo.outputPath("workspace-unread-restored.png"),
      clip: { x: 0, y: 0, width: 620, height: 470 },
    });
  } finally {
    await owner.deleteWorkspace();
    await viewer.deleteWorkspace();
  }
});
