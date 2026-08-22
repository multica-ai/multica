import "./env";

import { expect, test } from "@playwright/test";
import pg from "pg";

import { createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

const DATABASE_URL =
  process.env.DATABASE_URL ??
  "postgres://multica:***@localhost:5432/multica?sslmode=disable";

const ORIGINAL_HTML = [
  "<!doctype html>",
  '<html lang="en">',
  "<head>",
  '  <meta charset="utf-8">',
  "  <title>Live preview fixture</title>",
  "</head>",
  "<body>",
  "  <main>",
  "    <h1>Original preview</h1>",
  "    <p>Assistant generated HTML.</p>",
  "  </main>",
  "</body>",
  "</html>",
].join("\n");

const EDITED_HTML = ORIGINAL_HTML.replace("Original preview", "Edited live preview");

async function horizontalOverflowPx(page: import("@playwright/test").Page) {
  return page.evaluate(() => {
    const viewportWidth = document.documentElement.clientWidth;
    return Math.max(
      0,
      ...Array.from(document.querySelectorAll<HTMLElement>("body *"))
        .filter((element) => {
          const style = window.getComputedStyle(element);
          return style.display !== "none" && style.visibility !== "hidden";
        })
        .map((element) => Math.ceil(element.getBoundingClientRect().right - viewportWidth)),
    );
  });
}

test("edits an assistant HTML preview live without mutating message history", async ({
  page,
}) => {
  const api: TestApiClient = await createTestApi();
  const db = new pg.Client(DATABASE_URL);
  await db.connect();

  let sessionId: string | null = null;
  let agentId: string | null = null;
  let runtimeId: string | null = null;

  try {
    const workspace = (await api.getWorkspaces())[0];
    if (!workspace) throw new Error("E2E workspace missing");
    api.setWorkspaceId(workspace.id);
    api.setWorkspaceSlug(workspace.slug);

    const user = await db.query<{ id: string }>(
      `SELECT id::text FROM "user" WHERE email = $1 LIMIT 1`,
      [api.getEmail()],
    );
    const userId = user.rows[0]?.id;
    if (!userId) throw new Error("E2E user missing");

    const runtime = await db.query<{ id: string }>(
      `INSERT INTO agent_runtime (
         workspace_id, daemon_id, name, runtime_mode, provider, status,
         device_info, metadata, last_seen_at
       )
       VALUES ($1, NULL, $2, 'cloud', 'e2e_live_preview', 'online', $3, '{}'::jsonb, now())
       RETURNING id::text`,
      [workspace.id, `Live preview ${Date.now()}`, "Live preview E2E"],
    );
    runtimeId = runtime.rows[0]!.id;

    const agent = await db.query<{ id: string }>(
      `INSERT INTO agent (
         workspace_id, name, description, instructions, runtime_mode,
         runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id
       )
       VALUES ($1, 'Live Preview Agent', 'Generates preview fixtures', '', 'cloud',
               '{}'::jsonb, $2, 'workspace', 1, $3)
       RETURNING id::text`,
      [workspace.id, runtimeId, userId],
    );
    agentId = agent.rows[0]!.id;

    const session = await db.query<{ id: string }>(
      `INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status)
       VALUES ($1, $2, $3, 'Live preview workspace', 'active')
       RETURNING id::text`,
      [workspace.id, agentId, userId],
    );
    sessionId = session.rows[0]!.id;

    await db.query(
      `INSERT INTO chat_message (chat_session_id, role, content, created_at)
       VALUES
         ($1, 'user', 'Build a reviewable HTML concept.', now() - interval '1 second'),
         ($1, 'assistant', $2, now())`,
      [sessionId, ["```html", ORIGINAL_HTML, "```"].join("\n")],
    );

    const token = api.getToken();
    if (!token) throw new Error("E2E token missing");
    await page.addInitScript(
      ({ authToken, activeSessionId }) => {
        localStorage.setItem("multica_token", authToken);
        localStorage.setItem("multica:chat:activeSessionId", activeSessionId);
        localStorage.setItem("multica:chat:isOpen", "false");
      },
      { authToken: token, activeSessionId: sessionId },
    );

    await page.setViewportSize({ width: 1440, height: 960 });
    await page.goto(`/${workspace.slug}/chat?session=${sessionId}`, {
      waitUntil: "domcontentloaded",
    });

    const htmlBlock = page.locator(".code-block-wrapper").filter({
      has: page.locator('iframe[title="HTML preview"]'),
    });
    await expect(htmlBlock).toBeVisible({ timeout: 30_000 });
    await htmlBlock.hover();
    await page.getByRole("button", { name: "Edit live preview" }).click();

    const editor = page.getByRole("textbox", { name: "HTML editor" });
    await expect(editor).toHaveValue(ORIGINAL_HTML);
    await expect(page.getByText("Original", { exact: true })).toBeVisible();

    await editor.fill(EDITED_HTML);
    await expect(page.getByText("Local draft", { exact: true })).toBeVisible();
    const liveFrame = page.frameLocator('iframe[title="Live preview workspace"]');
    await expect(liveFrame.getByRole("heading", { name: "Edited live preview" })).toBeVisible();

    if (process.env.LIVE_PREVIEW_DESKTOP_SCREENSHOT_PATH) {
      await page.screenshot({
        path: process.env.LIVE_PREVIEW_DESKTOP_SCREENSHOT_PATH,
        fullPage: true,
      });
    }

    await page.getByRole("button", { name: "Reset changes" }).click();
    await expect(editor).toHaveValue(ORIGINAL_HTML);
    await expect(page.getByText("Original", { exact: true })).toBeVisible();
    await expect(liveFrame.getByRole("heading", { name: "Original preview" })).toBeVisible();

    await page.keyboard.press("Escape");
    await page.setViewportSize({ width: 390, height: 844 });
    await htmlBlock.hover();
    await page.getByRole("button", { name: "Edit live preview" }).click();
    const mobileEditor = page.getByRole("textbox", { name: "HTML editor" });
    await mobileEditor.fill(EDITED_HTML);
    const mobileFrame = page.frameLocator('iframe[title="Live preview workspace"]');
    await expect(mobileEditor).toBeVisible();
    await expect(mobileFrame.getByRole("heading", { name: "Edited live preview" })).toBeVisible();
    expect(await horizontalOverflowPx(page)).toBe(0);

    if (process.env.LIVE_PREVIEW_MOBILE_SCREENSHOT_PATH) {
      await page.screenshot({
        path: process.env.LIVE_PREVIEW_MOBILE_SCREENSHOT_PATH,
        fullPage: true,
      });
    }

    await page.keyboard.press("Escape");
    await page.setViewportSize({ width: 390, height: 430 });
    await htmlBlock.hover();
    await page.getByRole("button", { name: "Edit live preview" }).click();
    const shortEditor = page.getByRole("textbox", { name: "HTML editor" });
    await shortEditor.fill(EDITED_HTML);

    const shortPreviewElement = page.locator('iframe[title="Live preview workspace"]');
    await shortPreviewElement.scrollIntoViewIfNeeded();
    const shortFrame = page.frameLocator('iframe[title="Live preview workspace"]');
    await expect(
      shortFrame.getByRole("heading", { name: "Edited live preview" }),
    ).toBeVisible();

    if (process.env.LIVE_PREVIEW_SHORT_SCREENSHOT_PATH) {
      await page.screenshot({
        path: process.env.LIVE_PREVIEW_SHORT_SCREENSHOT_PATH,
        fullPage: true,
      });
    }

    await shortEditor.scrollIntoViewIfNeeded();
    await expect(shortEditor).toBeVisible();
    await shortEditor.fill(ORIGINAL_HTML);
    await expect(shortEditor).toHaveValue(ORIGINAL_HTML);
    expect(await horizontalOverflowPx(page)).toBe(0);

    const persisted = await db.query<{ content: string }>(
      `SELECT content FROM chat_message
       WHERE chat_session_id = $1 AND role = 'assistant'
       ORDER BY created_at DESC LIMIT 1`,
      [sessionId],
    );
    expect(persisted.rows[0]?.content).toContain("Original preview");
    expect(persisted.rows[0]?.content).not.toContain("Edited live preview");
  } finally {
    if (sessionId) {
      await db.query(`DELETE FROM agent_task_queue WHERE chat_session_id = $1`, [
        sessionId,
      ]);
      await db.query(`DELETE FROM chat_session WHERE id = $1`, [sessionId]);
    }
    if (agentId) await db.query(`DELETE FROM agent WHERE id = $1`, [agentId]);
    if (runtimeId)
      await db.query(`DELETE FROM agent_runtime WHERE id = $1`, [runtimeId]);
    await db.end();
    await api.cleanup();
  }
});
