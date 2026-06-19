// E2E test: Navigate to workflow overview from the workflow list.
//
// Verifies the full navigation path:
//   workflow list → workflow detail → overview tab
//
// Depends on: backend workflow API, frontend workflow pages.
// Expected to fail until the frontend implementation is built.

import { loginAsDefault, createTestApi } from "../helpers";
import { test, expect } from "../seed-workflow-overview";
import type { TestApiClient } from "../fixtures";

test.describe("Workflow Overview Navigation", () => {
  let api: TestApiClient;
  let slug: string;

  test.beforeEach(async ({ page }) => {
    // Login is handled by the seed fixture, but we need the API client for
    // data setup and the workspace slug for URL building.
    slug = await loginAsDefault(page);
    api = await createTestApi();
  });

  test.afterEach(async () => {
    if (api) {
      await api.cleanup();
    }
  });

  test("navigate from workflow list to overview page", async ({ page }) => {
    // Step 1: Navigate to the workflow list
    await page.goto(`/${slug}/workflows`);
    await page.waitForURL(`/${slug}/workflows`);
    await expect(page).toHaveURL(`/${slug}/workflows`);

    // Step 2: Ensure at least one workflow exists.
    // If the list is empty, seed a workflow via API and reload.
    const workflowCards = page.locator(
      'a[href*="/workflows/"]:not([href$="/workflows"])',
    );
    if (!(await workflowCards.first().isVisible({ timeout: 3000 }).catch(() => false))) {
      await api.createWorkflow("E2E Navigation Test Workflow " + Date.now());
      await page.reload();
      await page.waitForURL(`/${slug}/workflows`);
    }
    await expect(workflowCards.first()).toBeVisible({ timeout: 5000 });

    // Step 3: Click on the first workflow card to open its detail page
    await workflowCards.first().click();
    await page.waitForURL(`/${slug}/workflows/`);

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
    await page.waitForURL(`/${slug}/workflows/`);
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
