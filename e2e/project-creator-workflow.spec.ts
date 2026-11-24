import { test, expect } from "@playwright/test";
import { TestApiClient } from "./fixtures";

test.use({ locale: "en-US", colorScheme: "light", viewport: { width: 1440, height: 900 } });

test("a member can create a workflow and apply it to their project without being its lead", async ({ page, context, baseURL }, testInfo) => {
  const owner = new TestApiClient();
  const creator = new TestApiClient();
  const suffix = `${Date.now()}-${testInfo.workerIndex}`;
  const creatorEmail = `workflow-creator-${suffix}@multica.ai`;

  await owner.login(`workflow-owner-${suffix}@multica.ai`, "Workspace owner");
  const workspace = await owner.ensureWorkspace("Project workflows", `project-workflows-${suffix}`);
  try {
    await creator.login(creatorEmail, "Project creator");
    await owner.addMemberByEmail(creatorEmail);
    creator.setWorkspaceId(workspace.id);
    creator.setWorkspaceSlug(workspace.slug);
    await creator.markUserOnboarded();
    await context.addCookies([{ name: "multica-locale", value: "en", url: baseURL! }]);

    const project = await creator.createProject("My delivery project");
    const otherProject = await owner.createProject("A teammate's project");
    await creator.createIssue("Prepare the next release", { project_id: project.id, status: "todo" });
    const token = creator.getToken();
    expect(token).toBeTruthy();
    await page.addInitScript((value) => {
      localStorage.setItem("multica_token", value!);
      localStorage.setItem("multica:chat:isOpen", "false");
    }, token);

    await page.goto(`/${workspace.slug}/settings?tab=workflows&workflow=new`, { waitUntil: "domcontentloaded" });
    await page.getByRole("textbox", { name: "Name", exact: true }).fill("Personal delivery");
    const created = page.waitForResponse((response) =>
      response.url().endsWith("/api/issue-workflows") && response.request().method() === "POST",
    );
    await page.getByRole("button", { name: "Create", exact: true }).click();
    const response = await created;
    expect(response.status()).toBe(201);
    const workflow = await response.json();

    const openProjectFlow = async (target: typeof project) => {
      await page.goto(`/${workspace.slug}/projects/${target.id}`, { waitUntil: "domcontentloaded" });
      await page.getByRole("button", { name: target.title, exact: true }).click();
      await page.getByRole("menuitem", { name: /^Workflow/ }).click();
      return page.getByRole("dialog", { name: "Default workflow", exact: true });
    };

    const otherFlow = await openProjectFlow(otherProject);
    await expect(otherFlow).toBeVisible();
    await expect(otherFlow.getByRole("button", { name: "Switch workflow", exact: true })).toHaveCount(0);

    const ownFlow = await openProjectFlow(project);
    await expect(ownFlow.getByRole("button", { name: "Switch workflow", exact: true })).toBeVisible();
    const screenshot = testInfo.outputPath("creator-workflow-switch.png");
    await page.screenshot({ path: screenshot, animations: "disabled" });
    await testInfo.attach("Project creator can switch workflows", { path: screenshot, contentType: "image/png" });

    await ownFlow.getByRole("button", { name: "Switch workflow", exact: true }).click();
    await page.getByRole("menuitem", { name: /Personal delivery/ }).click();
    const confirmation = page.getByRole("dialog", { name: /Switch “My delivery project”/ });
    await expect(confirmation).toBeVisible();
    const switched = page.waitForResponse((result) =>
      result.url().endsWith(`/api/projects/${project.id}/workflow`) &&
      result.request().method() === "POST" && !result.request().postDataJSON().dry_run,
    );
    await confirmation.getByRole("button", { name: "Switch", exact: true }).click();
    const applied = await switched;
    expect(applied.status()).toBe(200);
    expect(await applied.json()).toMatchObject({
      project: { id: project.id, workflow_id: workflow.id },
      updated_issues: 0,
    });
    await expect(page.getByRole("dialog", { name: "Personal delivery workflow", exact: true })).toBeVisible();
    const appliedScreenshot = testInfo.outputPath("creator-workflow-applied.png");
    await page.screenshot({ path: appliedScreenshot, animations: "disabled" });
    await testInfo.attach("Creator's workflow applied", { path: appliedScreenshot, contentType: "image/png" });
  } finally {
    await creator.cleanup();
    await owner.cleanup();
    await owner.deleteWorkspace();
  }
});
