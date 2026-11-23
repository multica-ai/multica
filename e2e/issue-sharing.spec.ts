import { expect, test } from "@playwright/test";
import pg from "pg";
import { readFile } from "node:fs/promises";
import { API_BASE, TestApiClient } from "./fixtures";
import { createTestApi } from "./helpers";

test.use({ viewport: { width: 1440, height: 900 }, colorScheme: "light", locale: "en-US" });

test("shares one issue with an outside reader and revokes access", async ({ page, browser, baseURL }, testInfo) => {
  const owner = await createTestApi();
  const reader = new TestApiClient();
  const database = new pg.Client(process.env.DATABASE_URL);
  const readerContext = await browser.newContext({ baseURL, viewport: { width: 1440, height: 900 }, locale: "en-US" });
  const preview = await readerContext.newPage();
  const title = "Review the export fix";
  const description = "The CSV export fix is ready for review. Confirm the headers and download before releasing.";
  const request = "Please verify the CSV headers and confirm the download works after signing in.";
  const reply = "Both smoke checks passed. The CSV has the expected headers and downloads after sign-in.";
  const transcriptText = "Checked the CSV headers against the release checklist. The signed-in download succeeded.";
  const privateOutput = "Internal provider transcript";
  const filename = "export-checks.txt";
  const report = "CSV export smoke checks\nHeaders: passed\nSigned-in download: passed\n";
  const screenshotStyle = "nextjs-portal { display: none !important; }";
  let agentId: string | undefined;

  await database.connect();
  try {
    const [workspace] = await owner.getWorkspaces();
    const issue = await owner.createIssue(title, { description, status: "done" });
    const reviewComment = await owner.createComment(issue.id, request);
    const user = await database.query<{ id: string }>('SELECT id FROM "user" WHERE email = $1', [owner.getEmail()]);
    const agent = await database.query<{ id: string }>(`
      INSERT INTO agent (workspace_id, name, runtime_mode, visibility, permission_mode, owner_id, avatar_url)
      VALUES ($1, 'Release Review Agent', 'cloud', 'workspace', 'public_to', $2, 'emoji:🦊') RETURNING id
    `, [workspace!.id, user.rows[0]!.id]);
    agentId = agent.rows[0]!.id;
    await database.query(`
      INSERT INTO agent_invocation_target (agent_id, target_type, target_id)
      VALUES ($1, 'workspace', $2)
    `, [agentId, workspace!.id]);
    const run = await database.query<{ id: string }>(`
      INSERT INTO agent_task_queue (agent_id, issue_id, status, started_at, completed_at, result)
      VALUES ($1, $2, 'completed', now() - interval '2 minutes', now(), $3::jsonb) RETURNING id
    `, [agentId, issue.id, JSON.stringify({ output: privateOutput })]);
    const runId = run.rows[0]!.id;
    await database.query(`
      INSERT INTO task_message (task_id, seq, type, content)
      VALUES ($1, 1, 'text', $2), ($1, 2, 'thinking', $3)
    `, [runId, transcriptText, privateOutput]);
    await database.query(`
      INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content, source_task_id, parent_id)
      VALUES ($1, $2, 'agent', $3, $4, $5, $6)
    `, [workspace!.id, issue.id, agentId, reply, runId, reviewComment.id]);
    const file = new FormData();
    file.set("file", new Blob([report], { type: "text/plain" }), filename);
    file.set("issue_id", issue.id);
    file.set("comment_id", reviewComment.id);
    const upload = await fetch(`${API_BASE}/api/upload-file`, {
      method: "POST",
      headers: { Authorization: `Bearer ${owner.getToken()}`, "X-Workspace-Slug": workspace!.slug },
      body: file,
    });
    expect(upload.status).toBe(200);
    const attachment = await upload.json() as { id: string };
    await page.context().grantPermissions(["clipboard-read", "clipboard-write"]);
    await page.addInitScript((token) => {
      localStorage.setItem("multica_token", token ?? "");
    }, owner.getToken());
    await page.goto(`/${workspace!.slug}/issues/${issue.identifier}`);

    await expect(page.getByText(request)).toBeInViewport();
    const activityLog = page.getByRole("button", { name: "Execution log 1" });
    await expect(activityLog).toBeVisible();
    if (await activityLog.getAttribute("aria-expanded") === "false") await activityLog.click();
    await expect(page.getByText("Initial run", { exact: true })).toBeInViewport();
    await page.screenshot({ path: testInfo.outputPath("workspace-discussion-and-run.png"), style: screenshotStyle, animations: "disabled" });

    const menu = page.getByRole("button", { name: "More actions", exact: true });
    await menu.click();
    await page.getByRole("menuitem", { name: "Generate read-only link" }).click();
    const confirmation = page.getByRole("alertdialog", { name: "Generate read-only link" });
    await expect(confirmation.getByText(/people outside your workspace/)).toBeVisible();
    await expect(confirmation.getByText(/activity, execution logs, and deliverables/)).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath("generate-link.png"), style: screenshotStyle, animations: "disabled" });
    await confirmation.getByRole("button", { name: "Generate link" }).click();
    await expect(confirmation).toBeHidden();

    await menu.click();
    const copy = page.getByRole("menuitem", { name: "Copy read-only link" });
    await expect(copy).toBeVisible();
    await expect(page.getByRole("menuitem", { name: "Revoke read-only link" })).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath("copy-or-revoke.png"), style: screenshotStyle, animations: "disabled" });
    await copy.click();
    const link = await page.evaluate(() => navigator.clipboard.readText());
    expect(link).toMatch(/\/share\/issue#[a-f0-9]{24}$/);

    await preview.goto(link);
    await expect(preview).toHaveURL(/\/login\?next=%2Fshare%2Fissue$/);
    await reader.login(`outside-reviewer-${Date.now()}@multica.ai`, "Outside reviewer");
    expect(await reader.getWorkspaces()).toEqual([]);
    await preview.addInitScript((token) => {
      localStorage.setItem("multica_token", token ?? "");
    }, reader.getToken());
    await preview.reload();
    await expect(preview.getByRole("heading", { name: title })).toBeVisible();
    await expect(preview.getByText(description)).toBeVisible();
    await expect(preview.getByText("Shared issue · Read-only")).toBeVisible();
    const comments = preview.getByRole("region", { name: "Activity" });
    await expect(comments.getByText(request)).toBeVisible();
    await expect(comments.getByText(reply)).toBeVisible();
    await expect(comments.getByText("Release Review Agent", { exact: true })).toBeVisible();
    await expect(comments.getByText("🦊", { exact: true })).toBeVisible();
    const runs = preview.getByRole("region", { name: "Execution log" });
    await expect(runs.getByText("Completed")).toBeVisible();
    const deliverables = preview.getByRole("region", { name: "Deliverables" });
    const reportButton = deliverables.getByRole("button", { name: /export-checks\.txt/ });
    await expect(reportButton).toBeInViewport();
    await expect(preview.getByText(privateOutput)).toHaveCount(0);
    await preview.screenshot({ path: testInfo.outputPath("read-only-issue.png"), style: screenshotStyle, animations: "disabled" });

    await runs.getByRole("button", { name: "View transcript" }).click();
    const transcript = preview.getByRole("dialog", { name: "Agent execution transcript" });
    await expect(transcript.getByText(transcriptText)).toBeVisible();
    await expect(transcript.getByText(privateOutput)).toHaveCount(0);
    await preview.screenshot({ path: testInfo.outputPath("shared-execution-log.png"), style: screenshotStyle, animations: "disabled" });
    await preview.keyboard.press("Escape");

    await reportButton.click();
    const filePreview = preview.getByRole("dialog", { name: filename });
    await expect(filePreview.getByText("Signed-in download: passed", { exact: false })).toBeVisible();
    await preview.screenshot({ path: testInfo.outputPath("shared-deliverable-preview.png"), style: screenshotStyle, animations: "disabled" });
    const downloadPromise = preview.waitForEvent("download");
    await filePreview.getByRole("button", { name: "Download", exact: true }).click();
    const download = await downloadPromise;
    expect(download.suggestedFilename()).toBe(filename);
    expect(await readFile((await download.path())!, "utf8")).toBe(report);
    await preview.keyboard.press("Escape");

    await preview.setViewportSize({ width: 390, height: 844 });
    expect((await deliverables.boundingBox())!.y).toBeGreaterThan((await comments.boundingBox())!.y);
    await expect.poll(() => preview.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
    await preview.setViewportSize({ width: 1440, height: 900 });

    await menu.click();
    await page.getByRole("menuitem", { name: "Revoke read-only link" }).click();
    await page.getByRole("alertdialog", { name: "Revoke read-only link" }).getByRole("button", { name: "Revoke link" }).click();
    await expect(page.getByText("Read-only link revoked")).toBeVisible();
    await preview.reload();
    await expect(preview.getByRole("heading", { name: "This share link is unavailable" })).toBeVisible();
    await expect(preview.getByText(description)).toHaveCount(0);
    await expect(comments).toHaveCount(0);
    await expect(runs).toHaveCount(0);
    await expect(deliverables).toHaveCount(0);
    const headers = {
      Authorization: `Bearer ${reader.getToken()}`,
      "X-Issue-Share-Token": new URL(link).hash.slice(1),
    };
    for (const path of [`runs/${runId}/messages`, `deliverables/${attachment.id}`]) {
      expect((await fetch(`${API_BASE}/api/share/issue/${path}`, { headers })).status).toBe(404);
    }
  } finally {
    await readerContext.close();
    await owner.cleanup();
    await reader.cleanup();
    if (agentId) {
      await database.query("DELETE FROM agent_task_queue WHERE agent_id = $1", [agentId]);
      await database.query("DELETE FROM agent_invocation_target WHERE agent_id = $1", [agentId]);
      await database.query("DELETE FROM agent WHERE id = $1", [agentId]);
    }
    await database.end();
  }
});
