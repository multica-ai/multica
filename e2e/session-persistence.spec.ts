import { test, expect } from "@playwright/test";
import { loginWithCookieSession } from "./helpers";

test.describe("Session persistence", () => {
  test("recovers a cookie session after transient auth API unavailability", async ({
    page,
  }) => {
    const workspaceSlug = await loginWithCookieSession(page);

    await page.goto(`/${workspaceSlug}/issues`);
    await expect(page.getByText("All Issues")).toBeVisible({ timeout: 10000 });
    await expect(page.locator('input[placeholder="Email"]')).toHaveCount(0);
    await expect(
      page.evaluate(() => window.localStorage.getItem("multica_token")),
    ).resolves.toBeNull();

    let failedAuthProbe = false;
    await page.route("**/api/me", async (route) => {
      if (!failedAuthProbe) {
        failedAuthProbe = true;
        await route.fulfill({
          status: 503,
          contentType: "application/json",
          body: JSON.stringify({ error: "temporarily unavailable" }),
        });
        return;
      }
      await route.continue();
    });

    await page.reload();

    await expect(page).toHaveURL(new RegExp(`/${workspaceSlug}/issues`));
    await expect(page.locator('input[placeholder="Email"]')).toHaveCount(0);
    await expect(page.getByText("All Issues")).toBeVisible({ timeout: 15000 });
    expect(failedAuthProbe).toBe(true);
    await expect(
      page.evaluate(() => window.localStorage.getItem("multica_token")),
    ).resolves.toBeNull();
  });
});
