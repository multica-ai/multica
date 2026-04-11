import { test, expect } from "@playwright/test";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

test.describe("Projects", () => {
  let api: TestApiClient;
  let scope: string;

  test.beforeEach(async ({ page }, testInfo) => {
    scope = `projects-${testInfo.parallelIndex}-${testInfo.title}`;
    api = await createTestApi(scope);

    const existingProjects = await api.listProjects();
    for (const project of existingProjects.projects as Array<{ id: string }>) {
      await api.deleteProject(project.id);
    }

    await loginAsDefault(page, scope);
  });

  test.afterEach(async () => {
    if (api) {
      await api.cleanup();
    }
  });

  test("can create assign update filter and delete a project", async ({ page }) => {
    const issue = await api.createIssue(`E2E Project Flow Issue ${Date.now()}`);
    const projectTitle = `E2E Project ${Date.now()}`;

    await page.goto("/projects");
    await page.getByRole("button", { name: /^Create Project$/ }).last().click();
    await page.getByPlaceholder("e.g. Mobile app rollout").fill(projectTitle);
    await page.getByPlaceholder("What outcome does this project own?").fill("Project created from Playwright coverage.");
    await page.getByRole("button", { name: /^Create$/ }).click();

    await expect.poll(async () => {
      const result = await api.listProjects();
      const project = (result.projects as Array<{ id: string; title: string }>).find(
        (item) => item.title === projectTitle,
      );
      return project?.id ?? null;
    }).not.toBeNull();

    const projectsResult = await api.listProjects();
    const projectId = (projectsResult.projects as Array<{ id: string; title: string }>).find(
      (item) => item.title === projectTitle,
    )?.id;
    if (!projectId) {
      throw new Error("Missing project id after project creation");
    }

    api.trackProject(projectId);

    await expect(page.getByText("0 linked issues")).toBeVisible();

    await page.goto(`/issues/${issue.id}`);
    await page.getByRole("button", { name: "No project" }).click();
    await page.getByText(projectTitle).last().click();

    await expect.poll(async () => {
      const updatedIssue = await api.getIssue(issue.id);
      return updatedIssue.project_id;
    }).toBe(projectId);

    const filteredIssues = await api.listIssues({ project_id: projectId });
    expect(
      (filteredIssues.issues as Array<{ id: string }>).some((item) => item.id === issue.id),
    ).toBe(true);

    await page.goto(`/projects/${projectId}`);
    await expect(page.getByRole("link", { name: new RegExp(issue.title) })).toBeVisible();

    await page.getByRole("button", { name: "Project status" }).click();
    await page.getByRole("menuitem", { name: "In Progress" }).click();

    await expect.poll(async () => {
      const project = await api.getProject(projectId);
      return project.status;
    }).toBe("in_progress");

    const inProgressProjects = await api.listProjects({ status: "in_progress" });
    expect(
      (inProgressProjects.projects as Array<{ id: string }>).some((item) => item.id === projectId),
    ).toBe(true);

    await page.getByRole("button", { name: "Delete project" }).click();
    await page.getByRole("button", { name: /^Delete$/ }).click();
    await page.waitForURL("**/projects");

    await expect.poll(async () => {
      const updatedIssue = await api.getIssue(issue.id);
      return updatedIssue.project_id;
    }).toBe(null);
  });

  test("mobile projects page opens detail and returns to list", async ({ page }) => {
    const project = await api.createProject({
      title: `Mobile Project ${Date.now()}`,
      description: "Project used for mobile route coverage.",
    });

    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto("/projects");

    await expect(page.getByRole("heading", { name: "Projects" })).toBeVisible();
    await page.getByText(project.title).first().click();

    await page.waitForURL(new RegExp(`/projects/${project.id}$`));
    await expect(page.getByRole("button", { name: "Back to Projects" })).toBeVisible();
    await expect(page.getByText(project.title).first()).toBeVisible();

    await page.getByRole("button", { name: "Back to Projects" }).click();
    await expect(page).toHaveURL(/\/projects$/);
  });
});