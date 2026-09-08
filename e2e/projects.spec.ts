import { test, expect } from "@playwright/test";
import { createTestApi, loginAsDefault, waitForPageText } from "./helpers";
import type { TestApiClient } from "./fixtures";

test.describe("Projects", () => {
  let api: TestApiClient;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    await loginAsDefault(page);
  });

  test.afterEach(async () => {
    if (api) {
      await api.cleanup();
    }
  });

  test("can open a project and persist a status update", async ({ page }) => {
    const project = await api.createProject(`E2E Project ${Date.now()}`);

    await page.getByRole("link", { name: "Projects", exact: true }).click();
    await expect(page).toHaveURL(/\/projects$/);
    await waitForPageText(page, project.title);

    const projectRow = page.getByRole("row").filter({ hasText: project.title });
    await expect(projectRow).toBeVisible();
    await projectRow.getByText(project.title, { exact: true }).click();

    await expect(page).toHaveURL(new RegExp(`/projects/${project.id}$`));
    await waitForPageText(page, project.title);

    const propertiesSection = page
      .getByRole("button", { name: "Properties" })
      .locator("..");
    await propertiesSection.getByRole("button", { name: "Planned" }).click();

    const updateResponse = page.waitForResponse(
      (response) =>
        response.request().method() === "PUT" &&
        response.url().endsWith(`/api/projects/${project.id}`),
    );
    await page.getByRole("menuitem", { name: "In Progress" }).click();
    expect((await updateResponse).ok()).toBe(true);
    await expect(
      propertiesSection.getByRole("button", { name: "In Progress" }),
    ).toBeVisible();

    await page.reload({ waitUntil: "domcontentloaded" });
    await waitForPageText(page, project.title);
    await expect(
      page
        .getByRole("button", { name: "Properties" })
        .locator("..")
        .getByRole("button", { name: "In Progress" }),
    ).toBeVisible();
  });
});
