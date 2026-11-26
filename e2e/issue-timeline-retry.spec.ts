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

test("reopening an issue starts a fresh Activity read after leaving a stalled request", async ({ page }) => {
  test.setTimeout(120_000);
  const api = await createTestApi();
  let releaseStalled = () => {};
  const stalled = new Promise<void>((resolve) => { releaseStalled = resolve; });
  try {
    const workspace = (await api.getWorkspaces())[0]!;
    const title = "Reopen an issue after a stalled Activity request";
    const issue = await api.createIssue(title);
    await api.createComment(issue.id, "History loads after returning to the issue.");
    let timelineRequests = 0;
    let allowResponses = false;
    await page.route(`**/api/issues/${issue.id}/timeline*`, async (route) => {
      timelineRequests++;
      if (!allowResponses) {
        await stalled;
        await route.abort().catch(() => {});
      } else {
        await route.continue();
      }
    });
    await page.addInitScript((token) => {
      localStorage.setItem("multica_token", token!);
      localStorage.setItem("multica:chat:isOpen", "false");
    }, api.getToken());
    await page.goto(`/${workspace.slug}/issues`, { waitUntil: "domcontentloaded" });
    await page.getByText(title, { exact: true }).first().click();
    await expect.poll(() => timelineRequests).toBeGreaterThan(0);
    await expect(page).toHaveURL(new RegExp(`/issues/${issue.identifier}$`));
    await expect(page.getByRole("heading", { name: "Activity", exact: true })).toBeVisible();

    await page.goBack();
    await expect(page).toHaveURL(new RegExp(`/${workspace.slug}/issues$`));
    await expect(page.getByRole("heading", { name: "Activity", exact: true })).toBeHidden();
    const requestsBeforeReturn = timelineRequests;
    allowResponses = true;
    await page.getByText(title, { exact: true }).first().click();

    try {
      await expect(page.getByText("History loads after returning to the issue.", { exact: true })).toBeVisible({ timeout: 10_000 });
    } catch (error) {
      await page.screenshot({ path: test.info().outputPath("navigation-stalled.png") });
      throw error;
    }
    expect(timelineRequests).toBeGreaterThan(requestsBeforeReturn);
    await page.addStyleTag({ content: "nextjs-portal { display: none !important; }" });
    await page.screenshot({ path: test.info().outputPath("navigation-recovered.png") });
  } finally {
    releaseStalled();
    await api.cleanup();
  }
});
