// E2E test: Empty state when workflow has no stages.
//
// Seeds a workflow with zero stages, navigates to the overview page,
// and verifies:
//   1. Empty state message visible ("No stages defined yet" or Chinese equivalent)
//   2. CTA button "Create first stage" or similar is visible and clickable
//   3. Clicking the CTA opens the stage creation dialog
//
// Depends on: backend workflow API, frontend overview page.
// Expected to fail until the frontend implementation is built.

import { test, expect } from "../seed-workflow-overview";

test.describe("Empty State No Stages", () => {
  test("shows empty state message and create stage CTA when workflow has no stages", async ({
    page,
    slug,
    seededApi,
  }) => {
    // ── Setup: create a workflow with zero stages ──
    const workflow = await seededApi.createWorkflow(
      "E2E Empty Stages Test " + Date.now()
    );

    // Intentionally do NOT create any stages.
    // The workflow exists but has no stages defined.

    // ── Navigate to the overview page ──
    await page.goto(`/${slug}/workflows/${workflow.id}/overview`);
    await expect(page).toHaveURL(
      `/${slug}/workflows/${workflow.id}/overview`
    );

    // ── Step 1: Verify the main canvas container is present ──
    const stageCanvas = page.getByTestId("stage-canvas").or(
      page.locator('[class*="stage-canvas"]')
    );
    await expect(stageCanvas).toBeVisible({ timeout: 5000 });

    // ── Step 2: Verify empty state is visible ──
    // The empty state should show a message indicating no stages exist.
    // Check for the empty state container
    const emptyState = page
      .getByTestId("empty-stage-state")
      .or(page.locator('[class*="empty-stage"]'))
      .or(page.locator('[class*="empty-state"]'));

    await expect(emptyState).toBeVisible({ timeout: 5000 });

    // Verify the empty state message is shown
    // Match either English or Chinese text per the spec's healing hints
    const emptyMessage = emptyState.or(
      page.getByText(/No stages defined yet|尚未定义阶段|此工作流暂无阶段/)
    );
    // Wait for the text to appear within the empty state area
    await expect(
      page.locator(
        'text=/No stages defined yet|尚未定义阶段|此工作流暂无阶段|暂无阶段/'
      )
    ).toBeVisible({ timeout: 3000 });

    // ── Step 3: Verify CTA button is visible and clickable ──
    const ctaButton = page
      .getByRole("button", { name: /Create first stage|创建第一个阶段|创建阶段/ })
      .or(page.getByTestId("add-stage-button"));

    await expect(ctaButton).toBeVisible({ timeout: 3000 });
    await expect(ctaButton).toBeEnabled();

    // ── Step 4: Click the CTA button ──
    await ctaButton.click();

    // ── Step 5: Verify stage creation dialog opens ──
    // The dialog should be a modal or dialog component for creating a stage
    const dialog = page
      .getByRole("dialog")
      .or(page.getByTestId("stage-create-dialog"))
      .or(page.locator('[class*="dialog"]'));

    await expect(dialog).toBeVisible({ timeout: 3000 });

    // Verify the dialog has a title matching create stage
    await expect(
      dialog.locator(
        'text=/Create Stage|创建阶段|新建阶段/'
      )
    ).toBeVisible({ timeout: 2000 });

    // Verify the dialog contains a name input field
    const nameInput = dialog
      .getByLabel(/name|名称|阶段名称/)
      .or(dialog.locator('input[type="text"]').first());

    await expect(nameInput).toBeVisible({ timeout: 2000 });
  });
});
