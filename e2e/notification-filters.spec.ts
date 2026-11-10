import { test, expect } from "@playwright/test";
import type { TestApiClient } from "./fixtures";
import { createTestApi, loginAsDefault, waitForPageText } from "./helpers";

const FOCUSED_LABELS = [
  "I'm assigned to an issue",
  "My issue moves to Blocked",
  "I'm mentioned in a comment",
  "Someone replies in my thread",
  "An agent replies on my issue",
] as const;
const BLOCKED_LABEL = "My issue moves to Blocked";

test.describe("Notification filters", () => {
  let api: TestApiClient;

  test.beforeEach(async () => {
    api = await createTestApi();
  });

  test.afterEach(async () => {
    await api.notificationPreferences({ inbox_mode: "all", focused_blocked: "on" });
  });

  test("focused mode and its filters persist across reloads", async ({ page }) => {
    const slug = await loginAsDefault(page);
    await page.goto(`/${slug}/settings?tab=notifications`, { waitUntil: "domcontentloaded" });
    await waitForPageText(page, "Inbox mode");

    const modeGroup = page.getByRole("group", { name: "Inbox mode" });
    await modeGroup.getByRole("button", { name: "Focused", exact: true }).click();
    await expect(modeGroup.getByRole("button", { name: "Focused", exact: true })).toHaveAttribute(
      "aria-pressed",
      "true",
    );

    for (const label of FOCUSED_LABELS) {
      await expect(page.getByRole("checkbox", { name: label, exact: true })).toBeChecked();
    }

    await page.getByRole("checkbox", { name: BLOCKED_LABEL, exact: true }).click();
    await expect(page.getByRole("checkbox", { name: BLOCKED_LABEL, exact: true })).not.toBeChecked();
    // Wait for the PATCH to land before reloading.
    await expect
      .poll(async () => (await api.notificationPreferences()).focused_blocked)
      .toBe("off");

    await page.reload({ waitUntil: "domcontentloaded" });
    await waitForPageText(page, "Inbox mode");

    await expect(
      page.getByRole("group", { name: "Inbox mode" }).getByRole("button", { name: "Focused", exact: true }),
    ).toHaveAttribute("aria-pressed", "true");
    await expect(page.getByRole("checkbox", { name: BLOCKED_LABEL, exact: true })).not.toBeChecked();
    for (const label of FOCUSED_LABELS.filter((l) => l !== BLOCKED_LABEL)) {
      await expect(page.getByRole("checkbox", { name: label, exact: true })).toBeChecked();
    }

    const prefs = await api.notificationPreferences();
    expect(prefs.inbox_mode).toBe("focused");
    expect(prefs.focused_blocked).toBe("off");
  });
});
