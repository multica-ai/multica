import { test, expect } from "@playwright/test";
import { loginAsDefault, createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

/**
 * Project access E2E coverage.
 *
 * The regression these specs lock down: a previous deploy shipped without
 * `access` in `ProjectResponse`. The members panel never appeared after
 * flipping a project to Restricted because `project.access === undefined`,
 * and the restricted dot never showed in the projects list. Both flows are
 * exercised here.
 */

test.describe("Project access", () => {
  let api: TestApiClient;
  let slug: string;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    slug = await loginAsDefault(page);
  });

  test.afterEach(async () => {
    if (api) await api.cleanup();
  });

  test("API returns access field on list and detail responses", async () => {
    const project = await api.createProject(`E2E Access Field ${Date.now()}`);
    expect(project.access).toBe("workspace");

    const updated = await api.updateProjectAccess(project.id, "restricted");
    expect(updated.access).toBe("restricted");

    // List endpoint must also expose the field.
    const listRes = await fetch(
      `${process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080"}/api/projects`,
      {
        headers: {
          Authorization: `Bearer ${api.getToken()}`,
          "X-Workspace-Slug": slug,
        },
      },
    );
    const list = await listRes.json();
    const found = list.projects.find((p: { id: string }) => p.id === project.id);
    expect(found?.access).toBe("restricted");
  });

  test("restrict + pick members happens in one moment, members panel appears without reload", async ({ page }) => {
    const project = await api.createProject(`E2E Flip ${Date.now()}`);
    await page.goto(`/${slug}/projects/${project.id}`);

    // Open Access tab — the access tab is part of the project settings tabs.
    await page.getByRole("tab", { name: /access/i }).click();
    await expect(page.getByTestId("project-access-tab")).toBeVisible();

    // Initially "Open to workspace" is selected; no members panel.
    await expect(page.getByTestId("project-members-panel")).toHaveCount(0);

    // Pick Restricted radio → inline picker (not just a "review") appears.
    await page.getByRole("radio", { name: /Restricted/i }).click();
    await expect(page.getByTestId("access-review-panel")).toBeVisible();
    await expect(page.getByTestId("restrict-picker-list")).toBeVisible();

    // Confirm without picking anyone — admins-only is a valid state.
    await page.getByRole("button", { name: "Make restricted" }).click();

    // Members panel must appear without a manual reload.
    await expect(page.getByTestId("project-members-panel")).toBeVisible({
      timeout: 5000,
    });
    await expect(
      page.getByRole("heading", { name: "Project members" }),
    ).toBeVisible();
    await expect(
      page.getByRole("heading", { name: /always-on access/i }),
    ).toBeVisible();
  });

  test("restricted dot appears in projects list", async ({ page }) => {
    const title = `E2E Dot ${Date.now()}`;
    const project = await api.createProject(title);
    await api.updateProjectAccess(project.id, "restricted");

    await page.goto(`/${slug}/projects`);

    // The row containing the restricted project must show the dot.
    const row = page.getByTestId(`project-row-${project.id}`);
    await expect(row).toBeVisible();
    await expect(row.getByTestId("restricted-lock")).toBeVisible();
  });

  test("workspace-access projects do NOT show the dot", async ({ page }) => {
    const title = `E2E NoDot ${Date.now()}`;
    const project = await api.createProject(title);

    await page.goto(`/${slug}/projects`);

    const row = page.getByTestId(`project-row-${project.id}`);
    await expect(row).toBeVisible();
    await expect(row.getByTestId("restricted-lock")).toHaveCount(0);
  });

  test("toggling back to workspace hides the members panel", async ({ page }) => {
    const project = await api.createProject(`E2E Untoggle ${Date.now()}`);
    await api.updateProjectAccess(project.id, "restricted");

    await page.goto(`/${slug}/projects/${project.id}`);
    await page.getByRole("tab", { name: /access/i }).click();
    await expect(page.getByTestId("project-members-panel")).toBeVisible();

    await page.getByRole("radio", { name: /Open to workspace/i }).click();
    await expect(page.getByTestId("access-review-panel")).toBeVisible();
    await page.getByRole("button", { name: "Open to workspace" }).click();

    await expect(page.getByTestId("project-members-panel")).toHaveCount(0, {
      timeout: 5000,
    });
  });
});
