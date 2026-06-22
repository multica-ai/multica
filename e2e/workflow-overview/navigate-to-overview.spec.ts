// E2E test: Navigate to workflow overview from the workspace dashboard.
//
// Verifies the full navigation path:
//   dashboard → workflow list → workflow detail → overview tab
//
// Starts from the workspace dashboard (logged in via seed fixture),
// clicks through the sidebar, then drills into a workflow's overview.
//
// Depends on: backend workflow API, frontend workflow pages.
// Expected to fail until the frontend implementation is built.

import { test, expect } from "../seed-workflow-overview";

test.describe("Workflow Overview Navigation", () => {
  test("navigate from dashboard to workflow overview page", async ({ page, slug, seededApi }) => {
    // Step 1: The seed fixture has logged us in and landed on the dashboard
    // (/{slug}/issues). Click "Workflows" in the sidebar to navigate to the
    // workflow list.
    const workflowsNavLink = page.getByRole("link", { name: /workflows/i });
    await expect(workflowsNavLink.first()).toBeVisible({ timeout: 5000 });
    await workflowsNavLink.first().click();
    await page.waitForURL(`/${slug}/workflows`);

    // Step 2: Ensure at least one workflow exists.
    // If the list is empty, seed a workflow via API and reload.
    const workflowCards = page.locator(
      'a[href*="/workflows/"]:not([href$="/workflows"])',
    );
    if (!(await workflowCards.first().isVisible({ timeout: 3000 }).catch(() => false))) {
      await seededApi.createWorkflow("E2E Navigation Test Workflow " + Date.now());
      await page.reload();
    }
    await expect(workflowCards.first()).toBeVisible({ timeout: 5000 });

    // Step 3: Click on the first workflow card to open its detail page
    await workflowCards.first().click();
    await page.waitForURL(`/${slug}/workflows/*`);

    // Expect a workflow detail URL: /{slug}/workflows/{id}
    const workflowDetailPattern = new RegExp(`/${slug}/workflows/[a-f0-9-]+$`);
    await expect(page).toHaveURL(workflowDetailPattern);

    // Step 4: Click the "Overview" tab or navigation link.
    // The heal hint says: if "Overview" tab is renamed, check for link with
    // /overview href suffix.
    const overviewTab = page.getByRole("link", { name: /overview/i }).or(
      page.locator(`a[href$="/overview"]`),
    );
    await expect(overviewTab.first()).toBeVisible({ timeout: 5000 });
    await overviewTab.first().click();

    // Step 5: Expect the URL to match /{slug}/workflows/{id}/overview
    await page.waitForURL(`/${slug}/workflows/*/overview`);
    const overviewPattern = new RegExp(`/${slug}/workflows/[a-f0-9-]+/overview`);
    await expect(page).toHaveURL(overviewPattern);

    // Step 6: Expect the stage canvas area to be visible
    // The canvas area is the main content area for DAG visualization.
    // It may be rendered as a region with role "region" or a div with
    // test-id "stage-canvas" or a ReactFlow container.
    const stageCanvas = page.getByTestId("stage-canvas").or(
      page.locator(".react-flow"),
    );
    await expect(stageCanvas.first()).toBeVisible({ timeout: 5000 });

    // Step 7: Expect a page heading containing the workflow name
    const heading = page.getByRole("heading", { name: /workflow/i }).or(
      page.locator("h1"),
    );
    await expect(heading.first()).toBeVisible();
  });
});
