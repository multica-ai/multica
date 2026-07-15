import { test, expect } from "@playwright/test";
import pg from "pg";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

const databaseURL = process.env.DATABASE_URL!;

function captureBrowserFailures(page: import("@playwright/test").Page) {
  const consoleErrors: string[] = [];
  const failedApiResponses: string[] = [];
  page.on("console", (message) => {
    if (message.type() === "error") consoleErrors.push(message.text());
  });
  page.on("response", (response) => {
    if (response.url().includes("/api/") && response.status() >= 400) {
      failedApiResponses.push(`${response.status()} ${response.request().method()} ${response.url()}`);
    }
  });
  return () => {
    expect(consoleErrors, "browser console errors").toEqual([]);
    expect(failedApiResponses, "failed API responses").toEqual([]);
  };
}

test.describe("Cerebro session mode", () => {
  let api: TestApiClient;
  let issueId: string;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    await api.setWorkspaceFeatureFlag("cerebro_comment_chapters", true);
    await api.setWorkspaceFeatureFlag("cerebro_session_modes", true);
    const issue = await api.createIssue(`E2E Session Mode ${Date.now()}`);
    issueId = issue.id;
    await loginAsDefault(page);
  });

  test.afterEach(async () => {
    await api.setWorkspaceFeatureFlag("cerebro_comment_chapters", false).catch(() => {});
    await api.setWorkspaceFeatureFlag("cerebro_session_modes", false).catch(() => {});
    await api.cleanup();
  });

  test("starts the first issue thread in the selected Plan mode and keeps it after reload", async ({
    page,
  }) => {
    const expectBrowserClean = captureBrowserFailures(page);
    const issueLink = page.locator(`a[href$="/issues/${issueId}"]`);
    await expect(issueLink).toBeVisible({ timeout: 5000 });
    await issueLink.click();
    await page.waitForURL(/\/issues\/[\w-]+/);
    await expect(page.getByText("Properties")).toBeVisible();

    const newThreadMode = page.getByRole("combobox", { name: "New thread mode" });
    await expect(newThreadMode).toBeVisible();
    await expect(newThreadMode).toHaveText(/Build/);
    await newThreadMode.click();
    await page.getByRole("option", { name: "Plan" }).click();
    await expect(newThreadMode).toHaveText(/Plan/);

    const commentText = `Session mode comment ${Date.now()}`;
    const composer = page.getByTestId("composer-input").last();
    await composer.locator('[contenteditable="true"]').fill(commentText);
    await composer.getByRole("button", { name: "Submit" }).click();

    await expect(page.getByText(commentText).first()).toBeVisible({ timeout: 10000 });
    const sessionMode = page.getByRole("combobox", { name: "Session mode" }).last();
    await expect(sessionMode).toHaveText(/Plan/);

    await page.reload({ waitUntil: "domcontentloaded" });
    await expect(page.getByText(commentText).first()).toBeVisible({ timeout: 10000 });
    await expect(page.getByRole("combobox", { name: "Session mode" }).last()).toHaveText(/Plan/);
    expectBrowserClean();
  });

  test("publishes a versioned Mode from Settings and keeps it after reload", async ({ page }) => {
    const expectBrowserClean = captureBrowserFailures(page);
    const slug = api.getWorkspaceSlug();
    if (!slug) throw new Error("workspace slug is missing");
    await page.goto(`/${slug}/settings?tab=cerebro-modes`, { waitUntil: "domcontentloaded" });
    await expect(page.getByRole("heading", { name: "Modes" })).toBeVisible();

    const published = page.getByText(/Published version \d+/);
    const beforeText = await published.innerText();
    const before = Number(beforeText.match(/\d+/)?.[0] ?? "0");
    const instructions = page.getByLabel("Instructions");
    const original = await instructions.inputValue();
    const changed = `${original}\n\nE2E version marker ${Date.now()}`;
    await instructions.fill(changed);
    await page.getByLabel("Version note").fill("E2E settings verification");
    await page.getByRole("button", { name: "Publish version" }).click();
    await expect(page.getByRole("status")).toHaveText("Version published. New runs will use it.");
    await expect(published).toHaveText(`Published version ${before + 1}`);

    await page.reload({ waitUntil: "domcontentloaded" });
    await expect(page.getByLabel("Instructions")).toHaveValue(changed);

    const originalVersion = page.getByText(`Version ${before}`, { exact: true });
    const originalVersionRow = originalVersion.locator("..").locator("..");
    await originalVersionRow.getByRole("button", { name: "Restore" }).click();
    await expect(published).toHaveText(`Published version ${before + 2}`);
    await expect(page.getByLabel("Instructions")).toHaveValue(original);

    await page.reload({ waitUntil: "domcontentloaded" });
    await expect(page.getByLabel("Instructions")).toHaveValue(original);
    expectBrowserClean();
  });

  test("changes the Chat session Mode and keeps it after reload", async ({ page }) => {
    const expectBrowserClean = captureBrowserFailures(page);
    const workspaceId = api.getWorkspaceId();
    const userId = api.getUserId();
    const slug = api.getWorkspaceSlug();
    if (!workspaceId || !userId || !slug) throw new Error("E2E chat context is missing");

    const db = new pg.Client(databaseURL);
    await db.connect();
    let runtimeId = "";
    let agentId = "";
    let sessionId = "";
    try {
      runtimeId = (await db.query(
        `INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at)
         VALUES ($1, NULL, 'E2E Mode Runtime', 'cloud', 'e2e_mode_runtime', 'online', 'E2E', '{}', now()) RETURNING id`,
        [workspaceId],
      )).rows[0].id as string;
      agentId = (await db.query(
        `INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id)
         VALUES ($1, 'E2E Mode Agent', '', 'cloud', '{}', $2, 'workspace', 1, $3) RETURNING id`,
        [workspaceId, runtimeId, userId],
      )).rows[0].id as string;
      sessionId = (await db.query(
        `INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status, mode)
         VALUES ($1, $2, $3, 'E2E Chat Mode', 'active', 'build') RETURNING id`,
        [workspaceId, agentId, userId],
      )).rows[0].id as string;
      await db.query(
        "INSERT INTO chat_message (chat_session_id, role, content) VALUES ($1, 'user', 'Verify the Chat Mode selector')",
        [sessionId],
      );

      await page.goto(`/${slug}/inbox?chat=${sessionId}`, { waitUntil: "domcontentloaded" });
      const mode = page.getByRole("combobox", { name: "Session mode" });
      await expect(mode).toBeVisible({ timeout: 15000 });
      await expect(mode).toHaveText(/Build/);
      await mode.click();
      await page.getByRole("option", { name: "Research" }).click();
      await expect(mode).toHaveText(/Research/);

      await page.reload({ waitUntil: "domcontentloaded" });
      await expect(page.getByRole("combobox", { name: "Session mode" })).toHaveText(/Research/);
      expectBrowserClean();
    } finally {
      if (sessionId) await db.query("DELETE FROM chat_session WHERE id = $1", [sessionId]);
      if (agentId) await db.query("DELETE FROM agent WHERE id = $1", [agentId]);
      if (runtimeId) await db.query("DELETE FROM agent_runtime WHERE id = $1", [runtimeId]);
      await db.end();
    }
  });
});
