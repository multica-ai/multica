// FIR-2873 — Slack-style scheduled messages in the shared Channel/DM composer.

import { test, expect, type Locator, type Page, type TestInfo } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

const scheduleMenuLabels = [
  "Tomorrow at 9:00 AM",
  "Next Monday at 9:00 AM",
  "Custom time…",
  "Scheduled messages",
];

async function expectScheduleMenu(page: Page) {
  const menu = page.getByRole("menu");
  const positions: number[] = [];
  for (const label of scheduleMenuLabels) {
    const item = menu.getByRole("menuitem", { name: label });
    await expect(item).toBeVisible();
    await expect(item).toBeEnabled();
    const box = await item.boundingBox();
    if (!box) throw new Error(`Missing bounding box for ${label}`);
    positions.push(box.y);
  }
  expect(positions).toEqual([...positions].sort((a, b) => a - b));
  return menu;
}

async function captureFocused(
  page: Page,
  testInfo: TestInfo,
  name: string,
  locators: Locator[],
) {
  const boxes = await Promise.all(locators.map((locator) => locator.boundingBox()));
  if (boxes.some((box) => !box)) throw new Error(`Missing screenshot anchor for ${name}`);
  const resolved = boxes as NonNullable<(typeof boxes)[number]>[];
  const padding = 16;
  const viewport = page.viewportSize();
  if (!viewport) throw new Error(`Missing viewport for ${name}`);
  const x = Math.max(0, Math.min(...resolved.map((box) => box.x)) - padding);
  const y = Math.max(0, Math.min(...resolved.map((box) => box.y)) - padding);
  const right = Math.min(
    viewport.width,
    Math.max(...resolved.map((box) => box.x + box.width)) + padding,
  );
  const bottom = Math.min(
    viewport.height,
    Math.max(...resolved.map((box) => box.y + box.height)) + padding,
  );
  const path = testInfo.outputPath(`${name}.png`);
  await page.screenshot({ path, clip: { x, y, width: right - x, height: bottom - y } });
  await testInfo.attach(name, { path, contentType: "image/png" });
}

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

  test("schedules from the composer and manages the conversation queue", async ({ page }, testInfo) => {
    await page.goto(`/${workspaceSlug}/channels/${channelId}`);
    const composer = page.locator('[contenteditable="true"]').last();
    await composer.fill("Quarterly update is ready");

    const scheduleButton = page.getByRole("button", { name: "Schedule message" });
    await scheduleButton.click();
    const scheduleMenu = await expectScheduleMenu(page);
    await captureFocused(page, testInfo, "m1-channel-schedule-menu", [
      composer,
      scheduleButton,
      scheduleMenu,
    ]);
    await page.getByRole("menuitem", { name: "Tomorrow at 9:00 AM" }).click();
    await expect(page.getByText(/Message scheduled for/)).toBeVisible();
    await expect(composer).toHaveText("");

    // Slack-style confirmation offers a direct route to the queue.
    await page.getByRole("button", { name: "View" }).click();
    await expect(page.getByRole("heading", { name: "Scheduled messages" })).toBeVisible();
    await expect(page.getByText("Quarterly update is ready")).toBeVisible();
    await page.waitForTimeout(300); // Let the dialog fade-in finish before visual proof.
    const queueDialog = page.getByRole("dialog").filter({
      has: page.getByRole("heading", { name: "Scheduled messages" }),
    });
    await captureFocused(page, testInfo, "m2-scheduled-messages-queue", [queueDialog]);

    await page.getByRole("button", { name: "Scheduled message actions" }).click();
    const actionsMenu = page.getByRole("menu");
    for (const label of ["Edit or reschedule", "Send now", "Delete"]) {
      await expect(actionsMenu.getByRole("menuitem", { name: label })).toBeEnabled();
    }
    await captureFocused(page, testInfo, "m3-scheduled-message-actions", [queueDialog, actionsMenu]);
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
    const editDialog = page.getByRole("dialog").filter({
      has: page.getByRole("heading", { name: "Edit scheduled message" }),
    });
    await expect(page.getByRole("button", { name: "Save" })).toBeEnabled();
    await captureFocused(page, testInfo, "m4-edit-scheduled-message", [editDialog]);
    await page.getByRole("textbox", { name: "Message" }).fill("Edited and sent now");
    await page.getByRole("button", { name: "Save" }).click();
    await expect(page.getByText("Edited and sent now")).toBeVisible();
    await page.getByRole("button", { name: "Scheduled message actions" }).click();
    await page.getByRole("menuitem", { name: "Send now" }).click();
    await expect(page.getByText("No scheduled messages")).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(page.getByText("Edited and sent now")).toBeVisible();
  });

  test("schedules a DM preset and keeps it in the conversation queue after reload", async ({
    page,
  }, testInfo) => {
    const peer = await api.loginSecondaryUser(
      "fir-2873-dm-peer@multica.ai",
      "FIR-2873 DM Peer",
    );
    const directMessage = await api.createDirectMessage(peer.userId);

    await page.goto(`/${workspaceSlug}/channels/${directMessage.id}`);
    const composer = page.locator('[contenteditable="true"]').last();
    await composer.fill("Morning follow-up");

    const scheduleButton = page.getByRole("button", { name: "Schedule message" });
    await scheduleButton.click();
    const scheduleMenu = await expectScheduleMenu(page);
    await captureFocused(page, testInfo, "m5-dm-schedule-menu", [
      composer,
      scheduleButton,
      scheduleMenu,
    ]);
    await page.getByRole("menuitem", { name: "Tomorrow at 9:00 AM" }).click();
    await expect(page.getByText(/Message scheduled for/)).toBeVisible();

    await page.reload({ waitUntil: "domcontentloaded" });
    await page.getByRole("button", { name: "Schedule message" }).click();
    await page.getByRole("menuitem", { name: "Scheduled messages" }).click();
    await expect(
      page.getByRole("heading", { name: "Scheduled messages" }),
    ).toBeVisible();
    await expect(page.getByText("Morning follow-up", { exact: true })).toBeVisible();
  });

  test("stores a custom local time and keeps the same time after reload", async ({ page }) => {
    const customTime = "2099-01-15T09:30";

    await page.goto(`/${workspaceSlug}/channels/${channelId}`);
    const composer = page.locator('[contenteditable="true"]').last();
    await composer.fill("Custom-time follow-up");
    await page.getByRole("button", { name: "Schedule message" }).click();
    await page.getByRole("menuitem", { name: "Custom time…" }).click();
    await page.getByLabel("Send at").fill(customTime);
    await page.getByRole("button", { name: "Schedule", exact: true }).click();
    await expect(page.getByText(/Message scheduled for/)).toBeVisible();

    await page.reload({ waitUntil: "domcontentloaded" });
    await page.getByRole("button", { name: "Schedule message" }).click();
    await page.getByRole("menuitem", { name: "Scheduled messages" }).click();
    const expectedLocalTime = await page.evaluate(
      (value) => new Date(value).toLocaleString(),
      customTime,
    );
    await expect(
      page.getByText("Custom-time follow-up", { exact: true }),
    ).toBeVisible();
    await expect(page.getByText(expectedLocalTime, { exact: true })).toBeVisible();
  });

  test("delivers a scheduled thread reply under the same parent message", async ({ page }) => {
    const rootText = `Thread root ${Date.now()}`;
    const replyText = "Scheduled reply stays in this thread";
    await api.createComment(channelId, rootText);

    await page.goto(`/${workspaceSlug}/channels/${channelId}`);
    await page.getByText(rootText, { exact: true }).hover();
    await page.getByRole("button", { name: "Reply in thread" }).click();
    const threadPanel = page
      .locator("aside")
      .filter({ has: page.getByRole("heading", { name: "Thread" }) });
    await expect(threadPanel).toBeVisible();
    await threadPanel.locator('[contenteditable="true"]').fill(replyText);
    await threadPanel.getByRole("button", { name: "Schedule message" }).click();
    await page.getByRole("menuitem", { name: "Tomorrow at 9:00 AM" }).click();
    await page.getByRole("button", { name: "View" }).click();
    await page.getByRole("button", { name: "Scheduled message actions" }).click();
    await page.getByRole("menuitem", { name: "Send now" }).click();
    await expect(page.getByText("No scheduled messages")).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(threadPanel.getByText(replyText, { exact: true })).toBeVisible({
      timeout: 15_000,
    });
  });
});
