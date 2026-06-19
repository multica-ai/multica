// E2E test: User without workspace access sees "No access" page.
//
// Verifies that when a user who is NOT a member of the target workspace
// navigates to a workflow overview page, the "No access" page is shown
// and no workflow data is leaked in the DOM.
//
// Depends on: backend access control, frontend NoAccessPage.
// Expected to fail until the frontend implementation is built.

import { test, expect } from "../seed-workflow-overview";
import { TestApiClient } from "../fixtures";

test.describe("No Workspace Access", () => {
  let workflow: { id: string };

  test("non-member user sees NoAccessPage and no workflow data leaked", async ({
    page,
    slug,
    seededApi,
  }) => {
    // ── Setup: create a workflow with stages using the default user ──
    workflow = await seededApi.createWorkflow(
      "E2E No Access Test " + Date.now()
    );
    await seededApi.createWorkflowStage(workflow.id, "Research", 1);

    // ── Step 1: Log in as a different user who is NOT a workspace member ──
    // Create a separate API client with a unique email so we can log
    // the browser in as a non-member user.
    const outsiderEmail = `e2e-outsider-${Date.now()}@multica.ai`;
    const outsiderApi = new TestApiClient();
    await outsiderApi.login(outsiderEmail, "Outsider User");

    // Log in on the browser as this outsider user
    const outsiderToken = outsiderApi.getToken()!;
    await page.goto("/login");
    await page.evaluate((t) => {
      localStorage.setItem("multica_token", t);
    }, outsiderToken);

    // ── Step 2: Navigate to the workflow overview page of the default workspace ──
    await page.goto(`/${slug}/workflows/${workflow.id}/overview`);
    await page.waitForLoadState("networkidle");

    // ── Step 3: Verify "No access" / access denied page is displayed ──
    const noAccessMessage = page
      .getByTestId("no-access-page")
      .or(page.getByText(/no access|access denied|forbidden|无权访问|禁止访问/))
      .or(
        page.getByRole("heading", {
          name: /no access|access denied|forbidden|无权访问/,
        })
      )
      .or(page.locator('[class*="no-access"]'))
      .first();

    await expect(noAccessMessage).toBeVisible({ timeout: 5000 });

    // ── Step 4: Verify no workflow data is leaked in the DOM ──
    // Workflow-specific content like stage names or node titles should
    // NOT appear on the page
    const workflowDataInDom = page
      .getByText(/Research/i)
      .or(page.locator('[class*="stage-card"]'))
      .or(page.locator('[class*="react-flow"]'));

    await expect(workflowDataInDom).not.toBeVisible();

    // Verify the page is not a dashboard-like page with workflow content
    const pageTitle = await page.title();
    expect(pageTitle.toLowerCase()).not.toContain("overview");
    expect(pageTitle.toLowerCase()).not.toContain("workflow");
  });
});
