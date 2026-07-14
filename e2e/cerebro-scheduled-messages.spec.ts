// FIR-2873 — Slack-style scheduled messages in the shared Channel/DM composer.

import { test, expect } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

test.describe("Scheduled Channel and DM messages (FIR-2873)", () => {
  let api: TestApiClient;
  let channelId: string;
  let workspaceSlug: string;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    const channel = await api.createChannel(`FIR-2873 ${Date.now()}`);
    channelId = channel.id;
    workspaceSlug = await loginAsDefault(page);
  });

  test.afterEach(async () => { await api.cleanup(); });

  test("schedules from the composer and manages the conversation queue", async ({ page }) => {
    await page.goto(`/${workspaceSlug}/channels/${channelId}`);
    const composer = page.locator('[contenteditable="true"]').last();
    await composer.fill("Quarterly update is ready");

    await page.getByRole("button", { name: "Schedule message" }).click();
    await page.getByRole("menuitem", { name: "Tomorrow at 9:00 AM" }).click();
    await expect(page.getByText(/Message scheduled for/)).toBeVisible();
    await expect(composer).toHaveText("");

    // Slack-style confirmation offers a direct route to the queue.
    await page.getByRole("button", { name: "View" }).click();
    await expect(page.getByRole("heading", { name: "Scheduled messages" })).toBeVisible();
    await expect(page.getByText("Quarterly update is ready")).toBeVisible();
    await page.waitForTimeout(300); // Let the dialog fade-in finish before visual proof.
    await page.screenshot({ path: "test-results/fir-2873-scheduled-messages.png", fullPage: true });

    await page.getByRole("button", { name: "Scheduled message actions" }).click();
    await page.getByRole("menuitem", { name: "Delete" }).click();
    await expect(page.getByText("No scheduled messages")).toBeVisible();

    // Edit/reschedule and Send now use the same queue and canonical delivery path.
    await page.keyboard.press("Escape");
    await composer.fill("Send this now");
    await page.getByRole("button", { name: "Schedule message" }).click();
    await page.getByRole("menuitem", { name: "Tomorrow at 9:00 AM" }).click();
    await page.getByRole("button", { name: "View" }).click();
    await page.getByRole("button", { name: "Scheduled message actions" }).click();
    await page.getByRole("menuitem", { name: "Edit or reschedule" }).click();
    await page.getByRole("textbox", { name: "Message" }).fill("Edited and sent now");
    await page.getByRole("button", { name: "Save" }).click();
    await expect(page.getByText("Edited and sent now")).toBeVisible();
    await page.getByRole("button", { name: "Scheduled message actions" }).click();
    await page.getByRole("menuitem", { name: "Send now" }).click();
    await expect(page.getByText("No scheduled messages")).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(page.getByText("Edited and sent now")).toBeVisible();
  });
});
