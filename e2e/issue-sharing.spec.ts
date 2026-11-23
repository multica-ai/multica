import { expect, test } from "@playwright/test";
import { TestApiClient } from "./fixtures";
import { createTestApi } from "./helpers";

test.use({ viewport: { width: 1440, height: 900 }, colorScheme: "light", locale: "en-US" });

test("shares one issue with an outside reader and revokes access", async ({ page, browser, baseURL }, testInfo) => {
  const owner = await createTestApi();
  const reader = new TestApiClient();
  const readerContext = await browser.newContext({ baseURL, viewport: { width: 1440, height: 900 }, locale: "en-US" });
  const preview = await readerContext.newPage();
  const title = "Review the release checklist";
  const description = "The release candidate is ready for review. Confirm both smoke checks before publishing.\n\n• Sign in and open the release page.\n• Verify that generated reports download correctly.";
  const screenshotStyle = "nextjs-portal { display: none !important; }";

  try {
    const [workspace] = await owner.getWorkspaces();
    const issue = await owner.createIssue(title, { description });
    await page.context().grantPermissions(["clipboard-read", "clipboard-write"]);
    await page.addInitScript((token) => {
      localStorage.setItem("multica_token", token ?? "");
    }, owner.getToken());
    await page.goto(`/${workspace!.slug}/issues/${issue.identifier}`);

    const menu = page.getByRole("button", { name: "More actions", exact: true });
    await menu.click();
    await page.getByRole("menuitem", { name: "Generate read-only link" }).click();
    const confirmation = page.getByRole("alertdialog", { name: "Generate read-only link" });
    await expect(confirmation.getByText(/people outside your workspace/)).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath("generate-link.png"), style: screenshotStyle, animations: "disabled" });
    await confirmation.getByRole("button", { name: "Generate link" }).click();
    await expect(confirmation).toBeHidden();

    await menu.click();
    const copy = page.getByRole("menuitem", { name: "Copy read-only link" });
    await expect(copy).toBeVisible();
    await expect(page.getByRole("menuitem", { name: "Revoke read-only link" })).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath("copy-or-revoke.png"), style: screenshotStyle, animations: "disabled" });
    await copy.click();
    const link = await page.evaluate(() => navigator.clipboard.readText());
    expect(link).toMatch(/\/share\/issue#[a-f0-9]{24}$/);

    await preview.goto(link);
    await expect(preview).toHaveURL(/\/login\?next=%2Fshare%2Fissue$/);
    await reader.login(`outside-reviewer-${Date.now()}@multica.ai`, "Outside reviewer");
    expect(await reader.getWorkspaces()).toEqual([]);
    await preview.addInitScript((token) => {
      localStorage.setItem("multica_token", token ?? "");
    }, reader.getToken());
    await preview.reload();
    await expect(preview.getByRole("heading", { name: title })).toBeVisible();
    await expect(preview.getByText(description)).toBeVisible();
    await expect(preview.getByText("Shared issue · Read-only")).toBeVisible();
    await preview.screenshot({ path: testInfo.outputPath("read-only-issue.png"), style: screenshotStyle, animations: "disabled" });

    await menu.click();
    await page.getByRole("menuitem", { name: "Revoke read-only link" }).click();
    await page.getByRole("alertdialog", { name: "Revoke read-only link" }).getByRole("button", { name: "Revoke link" }).click();
    await expect(page.getByText("Read-only link revoked")).toBeVisible();
    await preview.reload();
    await expect(preview.getByRole("heading", { name: "This share link is unavailable" })).toBeVisible();
    await expect(preview.getByText(description)).toHaveCount(0);
  } finally {
    await readerContext.close();
    await owner.cleanup();
    await reader.cleanup();
  }
});
