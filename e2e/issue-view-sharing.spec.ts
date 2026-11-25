import { test, expect } from "@playwright/test";
import { TestApiClient } from "./fixtures";

test.use({ locale: "en-US", colorScheme: "light", viewport: { width: 1280, height: 900 } });

test("a member can share a project view from All views and make it private from the sidebar", async ({ page, browser }, testInfo) => {
  const owner = new TestApiClient();
  const creator = new TestApiClient();
  const reader = new TestApiClient();
  const run = `${Date.now()}-${testInfo.workerIndex}`;
  const slug = `view-sharing-${run}`;
  const apiBase = process.env.NEXT_PUBLIC_API_URL || `http://localhost:${process.env.PORT || "8080"}`;
  await owner.login(`view-owner-${run}@localhost`, "Workspace owner");
  const workspace = await owner.ensureWorkspace("View sharing", slug);
  expect(workspace.slug).toBe(slug);

  const readerContext = await browser.newContext();
  try {
    await creator.login(`view-creator-${run}@localhost`, "View creator");
    await reader.login(`view-reader-${run}@localhost`, "Project teammate");
    await owner.addMemberByEmail(creator.getEmail());
    await owner.addMemberByEmail(reader.getEmail());
    for (const member of [creator, reader]) {
      member.setWorkspaceId(workspace.id);
      member.setWorkspaceSlug(workspace.slug);
    }
    await creator.markUserOnboarded();
    await reader.markUserOnboarded();
    const project = await owner.createProject("Team project");
    const headers = (client: TestApiClient) => ({
      Authorization: `Bearer ${client.getToken()}`,
      "X-Workspace-ID": workspace.id,
    });
    const created = await page.request.post(`${apiBase}/api/issue-views`, {
      headers: headers(creator),
      data: {
        name: "Project review",
        scope_type: "project",
        scope_id: project.id,
        visibility: "private",
        definition_version: 1,
        query: {},
        display: {},
      },
    });
    expect(created.status()).toBe(201);
    const view = await created.json();
    const viewUrl = `${apiBase}/api/issue-views/${view.id}`;
    expect((await page.request.get(viewUrl, { headers: headers(reader) })).status()).toBe(404);

    await page.addInitScript((token) => {
      localStorage.setItem("multica_token", token!);
      localStorage.setItem("multica:chat:isOpen", "false");
    }, creator.getToken());
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.goto(`/${slug}/views`);
    const row = page.getByRole("row").filter({ hasText: "Project review" });
    const star = row.getByRole("button", { name: /Project review.*sidebar$/ });
    await expect(star).toBeVisible();
    if (await star.getAttribute("aria-pressed") !== "true") await star.click();
    await row.getByRole("button", { name: "Actions for Project review", exact: true }).click();
    await expect(page.getByRole("menuitem", { name: "Copy link", exact: true })).toBeVisible();
    await page.getByRole("menuitem", { name: /^Visible to\s*Only me/ }).click();
    await expect(page.getByRole("menuitemradio", { name: "Only me", exact: true })).toBeChecked();
    await page.screenshot({ path: testInfo.outputPath("directory-sharing-menu.png") });
    const shared = page.waitForResponse((res) => res.url().endsWith(`/api/issue-views/${view.id}`) && res.request().method() === "PATCH");
    await page.getByRole("menuitemradio", { name: "Workspace members", exact: true }).click();
    expect((await shared).status()).toBe(200);
    await expect(page.getByRole("menu")).toHaveCount(0);
    await page.reload();
    await expect(page.getByRole("row").filter({ hasText: "Project review" })).toContainText("Workspace members");

    const visible = await page.request.get(viewUrl, { headers: headers(reader) });
    expect(visible.status()).toBe(200);
    expect(await visible.json()).toMatchObject({ id: view.id, visibility: "workspace", scope_id: project.id });

    await readerContext.addInitScript((token) => {
      localStorage.setItem("multica_token", token!);
      localStorage.setItem("multica:chat:isOpen", "false");
    }, reader.getToken());
    const readerPage = await readerContext.newPage();
    const baseUrl = new URL(page.url()).origin;
    await readerPage.goto(`${baseUrl}/${slug}/views`);
    const readerRow = readerPage.getByRole("row").filter({ hasText: "Project review" });
    await readerRow.getByRole("button", { name: "Actions for Project review", exact: true }).click();
    await expect(readerPage.getByRole("menuitem", { name: "Copy link", exact: true })).toBeVisible();
    await expect(readerPage.getByRole("menuitem", { name: /^Visible to/ })).toHaveCount(0);
    await readerPage.keyboard.press("Escape");
    await readerRow.getByText("Project review", { exact: true }).click();
    await expect(readerPage).toHaveURL(new RegExp(`/projects/${project.id}\\?view=${view.id}$`));
    await readerPage.getByRole("tab", { name: /^Issues/ }).click();
    await expect(readerPage.getByRole("button", { name: "Project review", exact: true })).toBeVisible();
    await readerPage.screenshot({ path: testInfo.outputPath("teammate-shared-view.png") });

    await page.getByRole("link", { name: "Project review", exact: true }).click({ button: "right" });
    await page.getByRole("menuitem", { name: /^Visible to\s*Workspace members/ }).click();
    await expect(page.getByRole("menuitemradio", { name: "Workspace members", exact: true })).toBeChecked();
    await page.screenshot({ path: testInfo.outputPath("sidebar-sharing-menu.png") });
    const unshared = page.waitForResponse((res) => res.url().endsWith(`/api/issue-views/${view.id}`) && res.request().method() === "PATCH");
    await page.getByRole("menuitemradio", { name: "Only me", exact: true }).click();
    expect((await unshared).status()).toBe(200);
    expect((await page.request.get(viewUrl, { headers: headers(reader) })).status()).toBe(404);
    await readerPage.goto(`${baseUrl}/${slug}/views`);
    await expect(readerPage.getByText("Project review", { exact: true })).toHaveCount(0);
  } finally {
    await readerContext.close();
    await owner.cleanup();
    await owner.deleteWorkspace();
  }
});
