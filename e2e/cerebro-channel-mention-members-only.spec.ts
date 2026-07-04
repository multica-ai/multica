// FIR-2680 — channel mentions must respect membership.
//
// This is the end-to-end UI proof for the "add them to this channel?" prompt.
// It is NOT run by CI (the Playwright suite is manual / staging-only), so it is
// the script to run when enabling the `cerebro_channel_mention_members_only`
// flag on Firtal staging. Requires backend + frontend running and DATABASE_URL
// reachable (the fixtures seed the flag and read inbox rows directly).
//
// Covered outcomes:
//   1. Tagging a non-participant in a channel opens the "Add to this channel?"
//      dialog. "Send without" posts the message but delivers NO 'mentioned'
//      inbox row to the outsider (server guard drops it).
//   2. "Add & send" makes the outsider a participant and THEN the mention is
//      delivered (a 'mentioned' inbox row appears).

import { test, expect } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

const FLAG = "cerebro_channel_mention_members_only";

test.describe("Channel mentions respect membership (FIR-2680)", () => {
  let api: TestApiClient;
  let channelId: string;
  let outsiderId: string;
  let outsiderName: string;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    await api.setWorkspaceFeatureFlag(FLAG, true);

    // An outsider: a workspace member who is NOT a participant of the channel.
    outsiderName = `Outsider ${Date.now()}`;
    const outsider = await api.loginSecondaryUser(
      `fir2680-outsider-${Date.now()}@multica.ai`,
      outsiderName,
    );
    outsiderId = outsider.userId;

    const channel = await api.createChannel(`FIR-2680 ${Date.now()}`);
    channelId = channel.id;

    await loginAsDefault(page);
  });

  test.afterEach(async () => {
    await api.setWorkspaceFeatureFlag(FLAG, false);
    await api.cleanup();
  });

  // Types "@<name>" into the channel composer and picks the outsider from the
  // mention menu, then returns focus to the composer.
  async function mentionOutsider(page: import("@playwright/test").Page) {
    await page.goto(`/channels/${channelId}`);
    const composer = page.locator('[contenteditable="true"]').last();
    await composer.click();
    await page.keyboard.type(`@${outsiderName}`);
    // The mention menu renders each candidate as a <button> labelled by name.
    await page.getByRole("button", { name: outsiderName }).first().click();
  }

  test("'Send without' posts but does not notify the non-participant", async ({ page }) => {
    await mentionOutsider(page);
    await page.keyboard.press("Enter"); // submit

    await expect(page.getByText("Add to this channel?")).toBeVisible();
    await page.getByRole("button", { name: "Send without" }).click();

    // Give the synchronous notify path a beat to run, then assert no delivery.
    await expect
      .poll(async () => api.countMentionedRows(channelId, outsiderId), { timeout: 5000 })
      .toBe(0);
  });

  test("'Add & send' adds the participant and then delivers the mention", async ({ page }) => {
    await mentionOutsider(page);
    await page.keyboard.press("Enter"); // submit

    await expect(page.getByText("Add to this channel?")).toBeVisible();
    await page.getByRole("button", { name: "Add & send" }).click();

    await expect
      .poll(async () => api.countMentionedRows(channelId, outsiderId), { timeout: 5000 })
      .toBeGreaterThan(0);
  });
});
