import { test, expect } from "@playwright/test";
import { API_BASE, TestApiClient } from "./fixtures";

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

for (const entryPoint of ["workflow panel", "board column"] as const) {
  test(`a project creator saves a shared handoff only for their project from the ${entryPoint}`, async ({ page, context, request, baseURL }, testInfo) => {
    const owner = new TestApiClient();
    const creator = new TestApiClient();
    const suffix = `${Date.now()}-${testInfo.workerIndex}`;
    const creatorEmail = `handoff-creator-${suffix}@multica.ai`;
    const ownerSession = await owner.login(`handoff-owner-${suffix}@multica.ai`, "Workspace owner");
    const workspace = await owner.ensureWorkspace("Project handoffs", `project-handoffs-${suffix}`);

    try {
      const creatorSession = await creator.login(creatorEmail, "Project creator");
      await owner.addMemberByEmail(creatorEmail, "member");
      creator.setWorkspaceId(workspace.id);
      creator.setWorkspaceSlug(workspace.slug);
      await creator.markUserOnboarded();

      const headers = {
        Authorization: `Bearer ${creator.getToken()}`,
        "X-Workspace-ID": workspace.id,
      };
      const ownerHeaders = { ...headers, Authorization: `Bearer ${owner.getToken()}` };
      const members = await request.get(`${API_BASE}/api/workspaces/${workspace.id}/members`, { headers });
      expect(members.ok()).toBeTruthy();
      expect(await members.json()).toEqual(expect.arrayContaining([
        expect.objectContaining({ user_id: creatorSession.user.id, role: "member" }),
      ]));

      const project = await creator.createProject("Creator delivery", {
        lead_type: "member",
        lead_id: ownerSession.user.id,
      });
      expect(project).toMatchObject({
        creator_type: "member",
        creator_id: creatorSession.user.id,
        lead_type: "member",
        lead_id: ownerSession.user.id,
      });
      expect(creatorSession.user.id).not.toBe(ownerSession.user.id);
      const otherProject = await owner.createProject("Teammate delivery");

      const created = await request.post(`${API_BASE}/api/issue-workflows`, {
        headers,
        data: { name: "Shared delivery", steps: [{ status_key: "in_progress" }, { status_key: "in_review" }] },
      });
      expect(created.status()).toBe(201);
      const workflow = await created.json();
      expect(workflow.owner_id).toBe(creatorSession.user.id);
      for (const target of [project, otherProject]) {
        const switched = await request.post(`${API_BASE}/api/projects/${target.id}/workflow`, {
          headers: ownerHeaders,
          data: { workflow_id: workflow.id },
        });
        expect(switched.status()).toBe(200);
      }
      await creator.createIssue("Prepare the next release", { project_id: project.id, status: "todo" });

      await context.addCookies([{ name: "multica-locale", value: "en", url: baseURL! }]);
      await page.addInitScript((token) => {
        localStorage.setItem("multica_token", token!);
        localStorage.setItem("multica:chat:isOpen", "false");
      }, creator.getToken());
      await page.goto(
        `/${workspace.slug}/projects/${project.id}${entryPoint === "board column" ? "?tab=issues" : ""}`,
        { waitUntil: "domcontentloaded" },
      );

      if (entryPoint === "workflow panel") {
        await page.getByRole("button", { name: project.title, exact: true }).click();
        await page.getByRole("menuitem", { name: /^Workflow/ }).click();
        const panel = page.getByRole("dialog", { name: "Shared delivery workflow", exact: true });
        await expect(panel.getByText("2 projects use this workflow. Its handoffs apply to all of them.", { exact: true })).toBeVisible();
        await panel.getByRole("button", { name: /^In Progress/ }).click();
      } else {
        const column = page.locator('[data-mc="board-column"]').filter({
          has: page.getByText("In Progress", { exact: true }),
        });
        await column.hover();
        await column.getByRole("button", { name: "Hand to...", exact: true }).click();
        await expect(page.getByText("Part of Shared delivery, also used by Teammate delivery", { exact: true })).toBeVisible();
      }

      const form = page.getByRole("dialog").filter({
        has: page.getByRole("radiogroup", { name: "Hand to", exact: true }),
      });
      await expect(form.getByRole("radiogroup", { name: "Where should this change apply?" })).toHaveCount(0);
      await form.getByRole("radio", { name: /^Hand to a member/ }).click();
      await form.getByRole("button", { name: "Choose a member", exact: true }).click();
      await page.getByRole("button", { name: "Project lead", exact: true }).click();
      await expect(form.getByRole("radio", { name: /^All 2 projects using Shared delivery/ })).toBeVisible();
      const projectOnly = form.getByRole("radio", { name: /^Only Creator delivery/ });
      await projectOnly.click();
      await expect(projectOnly).toBeChecked();
      await expect(form.getByRole("button", { name: "Save", exact: true })).toBeEnabled();

      const captureName = entryPoint.replaceAll(" ", "-");
      const optionScreenshot = testInfo.outputPath(`creator-handoff-${captureName}-project-only.png`);
      await page.screenshot({ path: optionScreenshot, animations: "disabled" });
      await testInfo.attach(`Project-only handoff from the ${entryPoint}`, { path: optionScreenshot, contentType: "image/png" });

      const saving = page.waitForResponse((response) =>
        response.url().endsWith(`/api/issue-workflows/${workflow.id}/handoffs/in_progress`) &&
        response.request().method() === "PUT",
      );
      await form.getByRole("button", { name: "Save", exact: true }).click();
      const saved = await saving;
      expect(saved.request().postDataJSON()).toMatchObject({
        copy_for_project_id: project.id,
        handler: { type: "project_lead" },
      });
      expect(saved.status()).toBe(201);
      const copy = await saved.json();
      expect(copy.id).not.toBe(workflow.id);
      expect(copy.owner_id).toBe(creatorSession.user.id);
      expect(copy.steps).toEqual(expect.arrayContaining([
        expect.objectContaining({ status_key: "in_progress", handler: { type: "project_lead" } }),
      ]));
      const success = `Saved to “${copy.name}”, used only by ${project.title}`;
      await expect(page.getByText(success, { exact: true })).toBeVisible();
      if (entryPoint === "workflow panel") {
        await expect(page.getByRole("dialog", { name: `${copy.name} workflow`, exact: true })).toBeVisible();
      } else {
        await expect(form).toHaveCount(0);
      }
      const savedScreenshot = testInfo.outputPath(`creator-handoff-${captureName}-saved.png`);
      await page.screenshot({ path: savedScreenshot, animations: "disabled" });
      await testInfo.attach(`Project-only handoff saved from the ${entryPoint}`, { path: savedScreenshot, contentType: "image/png" });

      for (const [target, workflowId] of [[project, copy.id], [otherProject, workflow.id]] as const) {
        const result = await request.get(`${API_BASE}/api/projects/${target.id}`, { headers });
        expect(result.ok()).toBeTruthy();
        expect(await result.json()).toMatchObject({ id: target.id, workflow_id: workflowId });
      }
      const original = await request.get(`${API_BASE}/api/issue-workflows/${workflow.id}`, { headers });
      expect(original.ok()).toBeTruthy();
      expect(await original.json()).toMatchObject({
        project_ids: [otherProject.id],
        steps: workflow.steps,
      });
    } finally {
      await creator.cleanup();
      await owner.cleanup();
      await owner.deleteWorkspace();
    }
  });
}
