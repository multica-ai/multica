import { test, expect } from "@playwright/test";
import { TestApiClient } from "./fixtures";

test("members can delete their own shared project while keeping its issues", async ({ page }, testInfo) => {
  const owner = new TestApiClient();
  const member = new TestApiClient();
  const suffix = `${Date.now()}-${testInfo.workerIndex}`;
  const memberEmail = `project-member-${suffix}@multica.ai`;
  const apiBase = process.env.NEXT_PUBLIC_API_URL || `http://localhost:${process.env.PORT || "8080"}`;

  await owner.login(`project-owner-${suffix}@multica.ai`, "Project owner");
  const workspace = await owner.ensureWorkspace("Project permissions", `project-permissions-${suffix}`);
  try {
    await member.login(memberEmail, "Project member");
    await owner.addMemberByEmail(memberEmail);
    member.setWorkspaceId(workspace.id);
    member.setWorkspaceSlug(workspace.slug);
    await member.markUserOnboarded();

    const ownProject = await member.createProject("My temporary project");
    const otherProject = await owner.createProject("A teammate's project");
    const issue = await owner.createIssue("Keep this team's issue", { project_id: ownProject.id });
    const token = member.getToken();
    expect(token).toBeTruthy();
    await page.addInitScript((value) => {
      localStorage.setItem("multica_token", value!);
      localStorage.setItem("multica:chat:isOpen", "false");
    }, token);

    await page.goto(`/${workspace.slug}/projects/${otherProject.id}`, { waitUntil: "domcontentloaded" });
    await page.getByRole("button", { name: otherProject.title, exact: true }).click({ timeout: 30000 });
    await expect(page.getByRole("menuitem", { name: "Copy link", exact: true })).toBeVisible();
    await expect(page.getByRole("menuitem", { name: "Delete project", exact: true })).toHaveCount(0);

    const headers = { Authorization: `Bearer ${token}`, "X-Workspace-ID": workspace.id };
    const denied = await page.request.delete(`${apiBase}/api/projects/${otherProject.id}`, { headers });
    expect(denied.status()).toBe(403);

    await page.goto(`/${workspace.slug}/projects/${ownProject.id}`, { waitUntil: "domcontentloaded" });
    await page.getByRole("button", { name: ownProject.title, exact: true }).click();
    await expect(page.getByRole("menuitem", { name: "Delete project", exact: true })).toBeVisible();
    const screenshot = testInfo.outputPath("creator-delete-menu.png");
    await page.screenshot({ path: screenshot });
    await testInfo.attach("Creator can delete their project", { path: screenshot, contentType: "image/png" });
    await page.getByRole("menuitem", { name: "Delete project", exact: true }).click();
    const confirmation = page.getByRole("alertdialog", { name: "Delete project", exact: true });
    await expect(confirmation).toBeVisible();
    const deleted = page.waitForResponse((response) =>
      response.url().endsWith(`/api/projects/${ownProject.id}`) && response.request().method() === "DELETE",
    );
    await confirmation.getByRole("button", { name: "Delete", exact: true }).click();
    expect((await deleted).status()).toBe(204);
    await expect(page).toHaveURL(new RegExp(`/${workspace.slug}/projects$`));

    const retainedIssue = await page.request.get(`${apiBase}/api/issues/${issue.id}`, { headers });
    expect(retainedIssue.status()).toBe(200);
    expect(await retainedIssue.json()).toMatchObject({ id: issue.id, project_id: null });
  } finally {
    await member.cleanup();
    await owner.cleanup();
    await owner.deleteWorkspace();
  }
});
