// E2E test: Navigate to non-existent workflow — 404 handling.
//
// Verifies that navigating to a workflow overview for a workflow that does
// not exist shows a 404 / "not found" message, does not show an infinite
// loading skeleton, and does not white-screen.
//
// Depends on: backend 404 handling, frontend error boundary.
// Expected to fail until the frontend implementation is built.

import { test, expect } from "../seed-workflow-overview";

test.describe("Workflow Not Found (404)", () => {
  test("navigating to a non-existent workflow shows 404 and no white-screen", async ({
    page,
    slug,
  }) => {
    // ── Setup: navigate to an overview page for a non-existent workflow ──
    const nonExistentId = "00000000-0000-0000-0000-000000000000";

    await page.goto(`/${slug}/workflows/${nonExistentId}/overview`);
    await page.waitForLoadState("networkidle");

    // ── Step 1: Verify NOT showing a loading skeleton indefinitely ──
    // After network idle, a skeleton that persists means the page is broken.
    const skeleton = page
      .getByTestId("loading-skeleton")
      .or(page.locator('[class*="skeleton"]'))
      .or(page.locator('[class*="loading"]'));

    // If a skeleton is present, it should disappear within a timeout
    // that represents "not indefinitely" (similar to the app's timeouts)
    await expect(skeleton).not.toBeVisible({ timeout: 5000 });

    // ── Step 2: Verify page does NOT white-screen ──
    // See https://github.com/nicbarker/clay/issues/3118
    // White-screen means the page body is empty or only whitespace.
    const bodyContent = await page.evaluate(() => {
      const body = document.body;
      if (!body) return "no-body";
      const text = body.innerText?.trim() ?? "";
      return text.length > 0 ? "has-content" : "empty";
    });
    expect(bodyContent).not.toBe("empty");

    // ── Step 3: Verify 404 / "not found" message ──
    // The page should display a 404 or "not found" message
    const notFoundMessage = page
      .getByText(/not.?found|404|找不到|不存在/)
      .or(page.getByRole("heading", { name: /not.?found|404|找不到/ }))
      .or(page.locator('[data-testid="not-found"]'))
      .or(page.locator('[class*="not-found"]'))
      .first();

    await expect(notFoundMessage).toBeVisible({ timeout: 5000 });

    // ── Step 4: Verify page heading is not a workflow heading ──
    // The page should NOT show workflow-specific content
    const workflowHeading = page
      .getByRole("heading")
      .filter({ hasText: /overview|概览|workflow流程/ })
      .first();
    await expect(workflowHeading).not.toBeVisible();
  });
});
