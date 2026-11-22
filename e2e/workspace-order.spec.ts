import { test, expect } from "@playwright/test";
import { TestApiClient } from "./fixtures";

test("preferences reorder by keyboard and pointer follows the workspace switcher", async ({ page }, testInfo) => {
  const client = new TestApiClient();
  const secondOwner = new TestApiClient();
  const thirdOwner = new TestApiClient();
  const run = `${Date.now()}-${process.pid}`;
  await client.login(`workspace-order-${run}@localhost`, "Workspace order demo");
  const first = await client.ensureWorkspace("Atlas", `order-atlas-${run}`);
  await secondOwner.login(`workspace-order-second-${run}@localhost`, "Second demo owner");
  await thirdOwner.login(`workspace-order-third-${run}@localhost`, "Third demo owner");
  const second = await secondOwner.ensureWorkspace("Beacon", `order-beacon-${run}`);
  const third = await thirdOwner.ensureWorkspace("Cedar", `order-cedar-${run}`);
  await secondOwner.addMemberByEmail(`workspace-order-${run}@localhost`);
  await thirdOwner.addMemberByEmail(`workspace-order-${run}@localhost`);
  await client.markUserOnboarded();
  try {
    await page.addInitScript((token) => {
      localStorage.setItem("multica_token", token!);
      localStorage.setItem("multica:chat:isOpen", "false");
    }, client.getToken());
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.goto(`/${first.slug}/settings?tab=preferences&section=workspaces`, { waitUntil: "domcontentloaded" });
    const list = page.getByRole("list", { name: "Other workspaces" });
    const rows = list.locator("[data-workspace-order-id]");
    await expect(rows).toHaveCount(3, { timeout: 30000 });
    await expect(rows.nth(0)).toContainText("Atlas");
    await expect(rows.nth(1)).toContainText("Beacon");
    await expect(rows.nth(2)).toContainText("Cedar");
    const response = () => page.waitForResponse((r) => r.url().endsWith("/api/workspaces/order") && r.request().method() === "PUT");
    const keyboardSave = response();
    const handle = page.getByRole("button", { name: "Reorder Beacon", exact: true });
    await handle.focus();
    await page.keyboard.press("Space");
    await page.keyboard.press("ArrowUp");
    await page.keyboard.press("Space");
    expect((await keyboardSave).status()).toBe(204);
    await expect(rows.nth(0)).toContainText("Beacon");
    await expect(rows.nth(1)).toContainText("Atlas");
    await expect(rows.nth(2)).toContainText("Cedar");
    await expect(handle).toBeEnabled();

    const pointerSave = response();
    const source = await page.getByRole("button", { name: "Reorder Cedar", exact: true }).boundingBox();
    const target = await page.getByRole("button", { name: "Reorder Atlas", exact: true }).boundingBox();
    if (!source || !target) throw new Error("missing drag handles");
    await page.mouse.move(source.x + source.width / 2, source.y + source.height / 2);
    await page.mouse.down();
    await page.mouse.move(source.x + source.width / 2, source.y + source.height / 2 - 8, { steps: 3 });
    await page.mouse.move(target.x + target.width / 2, target.y + target.height / 2, { steps: 12 });
    await page.mouse.up();
    expect((await pointerSave).status()).toBe(204);
    await expect(rows.nth(0)).toContainText("Beacon");
    await expect(rows.nth(1)).toContainText("Cedar");
    await expect(rows.nth(2)).toContainText("Atlas");
    await page.screenshot({ path: testInfo.outputPath("workspace-order-preferences.png"), animations: "disabled" });

    await page.getByRole("button", { name: "Switch workspace", exact: true }).click();
    const menu = page.getByRole("menu");
    const workspaceItems = menu.getByRole("menuitem").filter({ hasText: /Atlas|Beacon|Cedar/ });
    await expect(workspaceItems.nth(0)).toContainText("Beacon");
    await expect(workspaceItems.nth(1)).toContainText("Cedar");
    await expect(workspaceItems.nth(2)).toContainText("Atlas");
    await page.screenshot({ path: testInfo.outputPath("workspace-order-switcher.png"), animations: "disabled" });
    await page.keyboard.press("Escape");
    await page.reload({ waitUntil: "domcontentloaded" });
    await expect(rows.nth(0)).toContainText("Beacon");
    await expect(rows.nth(1)).toContainText("Cedar");
    await expect(rows.nth(2)).toContainText("Atlas");
    expect((await client.getWorkspaces()).map((ws) => ws.id)).toEqual([second.id, third.id, first.id]);
  } finally {
    await thirdOwner.deleteWorkspace();
    await secondOwner.deleteWorkspace();
    await client.deleteWorkspace();
  }
});
