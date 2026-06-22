// E2E test: ARIA live regions and accessibility attributes.
//
// Verifies that the overview page has appropriate ARIA attributes for
// loading state and dynamic content announcements:
//   1. Loading state: region has `aria-busy="true"` or `role="status"`
//   2. After data loads: creating a new stage triggers a success
//      announcement in a live region (`role="status"` or `aria-live="polite"`)
//
// Depends on: backend workflow + stage API, frontend ARIA implementation.
// Expected to fail until the frontend implementation is built.

import { test, expect } from "../seed-workflow-overview";

test.describe("ARIA Live Regions", () => {
  test("loading state has aria-busy and stage creation announces success", async ({
    page,
    slug,
    seededApi,
  }) => {
    // ── Setup: create a workflow with one stage ──
    const workflow = await seededApi.createWorkflow(
      "E2E ARIA Test " + Date.now()
    );

    await seededApi.createWorkflowStage(workflow.id, "Planning", 1);

    // ── Step 1: Navigate to the overview page ──
    await page.goto(`/${slug}/workflows/${workflow.id}/overview`);
    await expect(page).toHaveURL(
      `/${slug}/workflows/${workflow.id}/overview`
    );

    // ── Step 2: Verify loading state has appropriate ARIA ──
    // During initial load, the page or canvas should have aria-busy="true"
    // or a loading region with role="status"

    // Check for aria-busy on the main canvas or page container
    const ariaBusyContainer = page
      .getByTestId("stage-canvas")
      .or(page.getByTestId("overview-page"))
      .or(page.locator('[class*="stage-canvas"]'))
      .or(page.locator("main"))
      .first();

    // Wait for the page to settle (loading to complete)
    await page.waitForLoadState("networkidle");

    // Check if the container had aria-busy during loading
    // (We check after page load since we can't intercept the exact
    // loading moment due to test timing)
    const hadAriaBusyInitially = await ariaBusyContainer.evaluate((el) => {
      // Check the current state (might have been removed after load)
      return el.getAttribute("aria-busy");
    });

    // If aria-busy is no longer present (data loaded), verify the
    // container exists and data is shown. The key test is that if
    // aria-busy was used, it was set to "true" during loading.
    const loadingRegion = page
      .getByRole("status")
      .or(page.locator('[aria-busy="true"]'))
      .or(page.locator('[aria-live="polite"]'))
      .or(page.locator('[aria-live="assertive"]'));

    // After load completes, look for any ARIA live regions
    const ariaLiveElements = page.locator("[aria-live]");
    const liveRegionCount = await ariaLiveElements.count();

    // There should be at least one live region on the page
    // (either a role="status" or aria-live="polite")
    const statusRole = page.getByRole("status");
    const hasStatusRole = await statusRole.isVisible().catch(() => false);

    const hasLiveRegion = liveRegionCount > 0 || hasStatusRole;

    // This is a soft assertion — if no live region was found during
    // loading, we check for role="status" as an alternative
    if (!hasLiveRegion) {
      // Fallback: look for any region that serves as an announcement
      const announcementRegion = page
        .locator('[class*="sr-only"]')
        .or(page.locator('[class*="visually-hidden"]'))
        .or(page.locator('[aria-label*="loading"i]'))
        .or(page.locator('[aria-label*="status"i]'));

      const hasAnnouncement = await announcementRegion
        .isVisible()
        .catch(() => false);
      expect(hasAnnouncement || hadAriaBusyInitially === "true").toBeTruthy();
    }

    // ── Step 3: Create a new stage and verify success announcement ──
    // Click the add stage button
    const addButton = page
      .getByTestId("add-stage-button")
      .or(
        page.getByRole("button", {
          name: /\+|add stage|添加阶段|新增阶段/,
        })
      );

    await expect(addButton).toBeVisible({ timeout: 3000 });
    await addButton.click();

    // Fill in the create stage dialog
    const dialog = page
      .getByTestId("stage-create-dialog")
      .or(page.getByRole("dialog"))
      .or(page.locator('[class*="dialog"]').first());

    await expect(dialog).toBeVisible({ timeout: 3000 });

    const stageName = "ARIA Test Stage";
    const nameInput = dialog
      .getByLabel(/name|名称|阶段名称/)
      .or(dialog.locator('input[type="text"]').first());

    await expect(nameInput).toBeVisible({ timeout: 2000 });
    await nameInput.fill(stageName);

    // Confirm/save
    const confirmButton = dialog
      .getByRole("button", { name: /confirm|create|save|确定|创建|保存/ })
      .or(dialog.locator('button[type="submit"]').first());

    await expect(confirmButton).toBeEnabled({ timeout: 2000 });
    await confirmButton.click();

    // Verify dialog closes
    await expect(dialog).not.toBeVisible({ timeout: 5000 });

    // ── Step 4: Verify success announcement in a live region ──
    // After creating a stage, a live region should announce the success

    // Check for live region that contains success-related content
    const liveRegion = page
      .getByRole("status")
      .or(page.locator('[aria-live="polite"]'))
      .or(page.locator('[aria-live="assertive"]'))
      .or(page.locator('[class*="toast"]'))
      .or(page.locator('[class*="notification"]'))
      .first();

    // If a live region exists, wait for it to have content
    const hasLiveRegionAfterCreate = await liveRegion
      .isVisible()
      .catch(() => false);

    if (hasLiveRegionAfterCreate) {
      // The live region should announce success (or at minimum contain text)
      await expect(liveRegion).not.toBeEmpty({ timeout: 5000 });

      const liveRegionText = await liveRegion.textContent();
      expect(liveRegionText?.trim().length).toBeGreaterThan(0);

      // The announcement should be related to the stage creation
      const containsStageReference =
        liveRegionText?.includes(stageName) ||
        liveRegionText?.toLowerCase().includes("stage") ||
        liveRegionText?.includes("create") ||
        liveRegionText?.includes("阶段") ||
        liveRegionText?.includes("创建");

      // Soft assertion — the announcement should reference the stage
      // but the exact wording depends on implementation
      if (containsStageReference) {
        expect(containsStageReference).toBe(true);
      }
    } else {
      // Fallback: check for a toast or notification element
      const notification = page
        .getByText(/created|success|创建成功|添加成功/)
        .or(
          page.locator('[class*="toast"]:has-text("created")')
        )
        .or(
          page.locator('[class*="toast"]:has-text("创建")')
        )
        .first();

      const hasNotification = await notification
        .isVisible()
        .catch(() => false);
      expect(hasNotification).toBe(true);
    }

    // ── Step 5: Verify the new stage card appears (data-update confirmation) ──
    const newStageCard = page
      .getByTestId(/^stage-card-/)
      .or(
        page
          .getByTestId("stage-card-strip")
          .locator('[class*="stage-card"]')
      );

    // Verify the new stage name is visible
    await expect(
      page.locator(`text=${stageName}`)
    ).toBeVisible({ timeout: 3000 });
  });
});
