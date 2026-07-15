import { test, expect, type Page } from "@playwright/test";
import { TestApiClient } from "./fixtures";

test.use({ viewport: { width: 1440, height: 900 } });

async function openFirtalWelcome(page: Page, email: string, name: string) {
  const api = new TestApiClient();
  await api.login(email, name);
  const token = api.getToken();

  await page.addInitScript((t) => {
    localStorage.setItem("multica_token", t);
  }, token);
  await page.goto("/onboarding", { waitUntil: "domcontentloaded" });
  await expect(
    page.getByRole("heading", { name: "Welcome to Firtal Multica" }),
  ).toBeVisible({ timeout: 15000 });
}

test("onboarding shows the Firtal member guide", async ({ page }) => {
  await openFirtalWelcome(
    page,
    `firtal-welcome-${Date.now()}@localhost`,
    "Welcome Tester",
  );

  await expect(
    page.getByRole("heading", { name: "1. What can you do as a member?" }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "2. Download the desktop app (recommended)" }),
  ).toBeVisible();
  await expect(page.getByRole("link", { name: "Download desktop app" })).toHaveAttribute(
    "href",
    "/download",
  );
  await expect(page.getByRole("button", { name: "Go to my inbox" })).toBeDisabled();
});

test("onboarding requires support acknowledgement before continuing", async ({ page }) => {
  await openFirtalWelcome(
    page,
    `support-gate-${Date.now()}@localhost`,
    "Support Gate Tester",
  );

  const continueButton = page.getByRole("button", { name: "Go to my inbox" });
  await expect(continueButton).toBeDisabled();

  await page.getByRole("radio", { name: /I have read and understood/ }).click();
  await expect(continueButton).toBeEnabled();
  await continueButton.click();

  await expect
    .poll(() =>
      page.evaluate(() =>
        Object.entries(localStorage).some(
          ([key, value]) =>
            key.startsWith("cerebro_firtal_welcome_seen:") && value === "1",
        ),
      ),
    )
    .toBe(true);
});

test("onboarding lets the user log out", async ({ page }) => {
  await openFirtalWelcome(
    page,
    `welcome-logout-${Date.now()}@localhost`,
    "Welcome Logout Tester",
  );

  await page.getByRole("button", { name: "Log out" }).click();
  await page.waitForURL("**/login", { timeout: 10000 });
});
