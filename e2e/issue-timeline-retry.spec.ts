import { expect, test } from "@playwright/test";
import { createTestApi } from "./helpers";

test.use({ viewport: { width: 1440, height: 900 } });

test("a failed Activity read can be retried without reopening the issue", async ({ page }) => {
  test.setTimeout(120_000);
  const api = await createTestApi();
  try {
    const workspace = (await api.getWorkspaces())[0]!;
    const issue = await api.createIssue("Recover an unavailable Activity history");
    await api.createComment(issue.id, "The saved comment is available again.");
    let failTimeline = true;
    await page.route(`**/api/issues/${issue.id}/timeline*`, async (route) => {
      if (failTimeline) {
        await route.fulfill({ status: 503, json: { error: "Temporary timeline failure" } });
      } else {
        await route.continue();
      }
    });
    await page.addInitScript((token) => {
      localStorage.setItem("multica_token", token!);
      localStorage.setItem("multica:chat:isOpen", "false");
    }, api.getToken());
    await page.goto(`/${workspace.slug}/issues/${issue.identifier}`, { waitUntil: "domcontentloaded" });

    await expect(page.getByText("Could not load activity.", { exact: true })).toBeVisible({ timeout: 60_000 });
    await page.addStyleTag({ content: "nextjs-portal { display: none !important; }" });
    await page.getByText("Could not load activity.", { exact: true }).scrollIntoViewIfNeeded();
    await page.screenshot({ path: test.info().outputPath("activity-error.png") });

    failTimeline = false;
    await page.getByRole("button", { name: "Try again", exact: true }).click();

    await expect(page.getByText("The saved comment is available again.", { exact: true })).toBeVisible();
    await expect(page.getByText("Could not load activity.", { exact: true })).toBeHidden();
    await page.screenshot({ path: test.info().outputPath("activity-recovered.png") });
  } finally {
    await api.cleanup();
  }
});
