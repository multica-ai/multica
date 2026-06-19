// Seed test for workflow stage overview feature.
// All overview scenarios assume a logged-in user on a workspace-scoped
// workflow overview page with pre-seeded data.
//
// This file exports a `test` fixture that handles login and attempts to
// navigate to a workflow overview page. Tests that need a specific starting
// page (e.g. the workflow list) can navigate there within the test body —
// the fixture guarantees the user is authenticated.

import { test as baseTest, expect } from "@playwright/test";
import { loginAsDefault, createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

const test = baseTest.extend<{ seededApi: TestApiClient }>({
  seededApi: async ({}, use) => {
    const api = await createTestApi();
    await use(api);
    await api.cleanup();
  },

  page: async ({ page }, use) => {
    const api = await createTestApi();
    const slug = await loginAsDefault(page);

    // Navigate to the first available workflow's overview page
    await page.goto(`/${slug}/workflows`);

    // Try to find a workflow to navigate to its overview page
    const workflowLink = page.locator('a[href*="/workflows/"]').first();
    if (await workflowLink.isVisible({ timeout: 3000 }).catch(() => false)) {
      const href = await workflowLink.getAttribute("href");
      const workflowId = href?.split("/workflows/")[1]?.split("/")[0];
      if (workflowId) {
        await page.goto(`/${slug}/workflows/${workflowId}/overview`);
      }
    }

    await use(page);

    await api.cleanup();
  },
});

export { test, expect };
