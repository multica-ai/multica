import { expect, test, type Page } from "@playwright/test";

import type { TestApiClient } from "./fixtures";
import { createTestApi, loginAsDefault } from "./helpers";

async function openProjectsTable(page: Page, slug: string) {
  await page.goto(`/${slug}/projects`);
  await expect(page.getByRole("heading", { name: "Projects" })).toBeVisible();

  const switchToTable = page.getByRole("button", { name: "Table", exact: true });
  if (await switchToTable.isVisible()) {
    await switchToTable.click();
  }
  await expect(page.getByRole("button", { name: "Cards", exact: true })).toBeVisible();
}

function projectRow(page: Page, title: string) {
  return page
    .getByRole("link", { name: title, exact: true })
    .locator("xpath=ancestor::div[contains(concat(' ', normalize-space(@class), ' '), ' group/row ')]");
}

test.describe("FIR-3425 Projects tree table", () => {
  let api: TestApiClient;
  let slug: string;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    await api.setWorkspaceFeatureFlag("cerebro_projects_tree", true);
    await api.setWorkspaceFeatureFlag("cerebro_sprints", true);
    slug = await loginAsDefault(page);
  });

  test.afterEach(async () => {
    if (!api) return;
    await api.setWorkspaceFeatureFlag("cerebro_projects_tree", false);
    await api.setWorkspaceFeatureFlag("cerebro_sprints", false);
    await api.cleanup();
  });

  test("keeps a project branch collapsed after reload", async ({ page }) => {
    const suffix = Date.now();
    const parentTitle = `E2E Tree Parent ${suffix}`;
    const childTitle = `E2E Tree Child ${suffix}`;
    const parent = await api.createProject(parentTitle);
    await api.createProject(childTitle, { parent_project_id: parent.id });

    await openProjectsTable(page, slug);

    const childLink = page.getByRole("link", { name: childTitle, exact: true });
    await expect(childLink).toBeVisible();
    await projectRow(page, parentTitle)
      .getByRole("button", { name: "Collapse project" })
      .click();
    await expect(childLink).toBeHidden();

    await page.reload();
    await expect(page.getByRole("heading", { name: "Projects" })).toBeVisible();
    await expect(childLink).toBeHidden();

    await projectRow(page, parentTitle)
      .getByRole("button", { name: "Expand project" })
      .click();
    await expect(childLink).toBeVisible();
  });

  test("opens a sprint from its nested table row", async ({ page }) => {
    const suffix = Date.now();
    const project = await api.createProject(`E2E Sprint Project ${suffix}`);
    const sprintName = `E2E Active Sprint ${suffix}`;
    const sprint = await api.createProjectSprint(project.id, {
      name: sprintName,
      start_date: "2026-07-13",
      end_date: "2026-07-26",
      status: "active",
    });

    await openProjectsTable(page, slug);
    await page.getByRole("link", { name: new RegExp(`^${sprintName}`) }).click();

    await expect(page).toHaveURL(`/${slug}/sprints/${sprint.id}`);
  });
});
