/**
 * E2E: dismissing the floating chat hands keyboard focus back to the page.
 *
 * Issue #8994. The floating window is never unmounted while closed, only hidden
 * by `opacity` and `pointer-events`, so before the fix the composer kept
 * `document.activeElement`. Global shortcuts that refuse to fire from an
 * editable target then went dead: `C` (createIssue) did nothing, and the
 * keystroke was typed into the invisible composer instead.
 *
 * Two signals are used deliberately. Playwright's visibility check does not
 * report an `opacity: 0` element as hidden, so the toggle is read from the
 * store's persisted `multica:chat:isOpen` flag, and the focus half is read from
 * `document.activeElement` — which is the thing the shortcut rule actually
 * consults (`isEditableShortcutTarget`).
 */
import "./env";
import { test, expect, type Page } from "@playwright/test";
import pg from "pg";
import { createTestApi, loginAsDefault, preferManualCreateMode } from "./helpers";
import type { TestApiClient } from "./fixtures";

const DATABASE_URL =
  process.env.DATABASE_URL ?? "postgres://multica:multica@localhost:5432/multica?sslmode=disable";

/** What the global shortcut handler asks before running `createIssue`. */
function focusIsInEditable(page: Page): Promise<boolean> {
  return page.evaluate(() => {
    const el = document.activeElement;
    if (!(el instanceof HTMLElement)) return false;
    return (
      el.isContentEditable ||
      el.tagName === "INPUT" ||
      el.tagName === "TEXTAREA" ||
      el.tagName === "SELECT" ||
      el.closest("[contenteditable='true']") !== null
    );
  });
}

function chatIsOpen(page: Page): Promise<string | null> {
  return page.evaluate(() => localStorage.getItem("multica:chat:isOpen"));
}

test.describe("Floating chat focus", () => {
  let api: TestApiClient;
  let pgClient: pg.Client;
  let createdAgentId: string | null = null;
  let createdRuntimeId: string | null = null;

  test.beforeEach(async () => {
    api = await createTestApi();
    pgClient = new pg.Client(DATABASE_URL);
    await pgClient.connect();
  });

  test.afterEach(async () => {
    try {
      if (createdAgentId) await pgClient.query(`DELETE FROM agent WHERE id = $1`, [createdAgentId]);
      if (createdRuntimeId) {
        await pgClient.query(`DELETE FROM agent_runtime WHERE id = $1`, [createdRuntimeId]);
      }
    } finally {
      createdAgentId = null;
      createdRuntimeId = null;
      await pgClient.end();
      await api.cleanup();
    }
  });

  test("closing the chat releases the composer, so C still creates an issue", async ({ page }) => {
    const slug = await loginAsDefault(page);
    const ws = (await api.getWorkspaces()).find((w) => w.slug === slug);
    if (!ws) throw new Error("e2e workspace missing");

    // The composer is disabled unless its agent has a bound runtime, and
    // `isAgentRuntimeBound` only asks for a non-empty runtime_id — no daemon
    // has to be online for this. Seeded directly, as chat-attachments.spec.ts
    // does, because going through the API would mean modelling local-daemon
    // ownership that this test does not need.
    const userRow = await pgClient.query(`SELECT id FROM "user" WHERE email = $1 LIMIT 1`, [
      api.getEmail(),
    ]);
    if (userRow.rows.length === 0) throw new Error("e2e user missing");
    const userId = userRow.rows[0].id as string;

    const runtimeIns = await pgClient.query(
      `INSERT INTO agent_runtime (
         workspace_id, daemon_id, name, runtime_mode, provider, status,
         device_info, metadata, last_seen_at
       )
       VALUES ($1, NULL, $2, 'cloud', $3, 'online', $4, '{}'::jsonb, now())
       RETURNING id`,
      [ws.id, `e2e focus runtime ${Date.now()}`, "e2e_focus_runtime", "E2E focus runtime"],
    );
    createdRuntimeId = runtimeIns.rows[0].id as string;

    const agentIns = await pgClient.query(
      `INSERT INTO agent (
         workspace_id, name, description, runtime_mode, runtime_config,
         runtime_id, visibility, max_concurrent_tasks, owner_id
       )
       VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
       RETURNING id`,
      [ws.id, `E2E Focus Agent ${Date.now()}`, createdRuntimeId, userId],
    );
    createdAgentId = agentIns.rows[0].id as string;

    // Reload so the seeded agent is in the picker the chat window resolves from,
    // and so the create dialog opens in manual mode deterministically.
    await preferManualCreateMode(page);
    expect(await chatIsOpen(page)).toBe("false");

    // 1. Open the floating chat. The composer takes focus by design (MUL-5522).
    await page.keyboard.press("ControlOrMeta+j");
    await expect.poll(() => chatIsOpen(page)).toBe("true");
    await expect.poll(() => focusIsInEditable(page)).toBe(true);

    // 2. Dismiss it. Nothing likes a hidden element that still owns the caret.
    await page.keyboard.press("ControlOrMeta+j");
    await expect.poll(() => chatIsOpen(page)).toBe("false");
    await expect.poll(() => focusIsInEditable(page)).toBe(false);

    // 3. The shortcut that used to be dead. `createIssue` is allowInEditable:
    //    false, so it only fires once the caret is out of the composer.
    await page.keyboard.press("c");
    await expect(page.getByRole("textbox", { name: "Issue title" })).toBeVisible();
  });
});