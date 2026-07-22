import { test, expect } from "@playwright/test";
import pg from "pg";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

// MUL-5122 — the paginated + virtualized Execution Log dialog. These are the two
// acceptance criteria that can only be proven in a real browser: a 10,000-event
// Run renders a BOUNDED number of DOM rows (virtualization), and loading older
// history does not move the row the reader is looking at (scroll anchor).

const DATABASE_URL =
  process.env.DATABASE_URL ??
  "postgres://multica:multica@localhost:5432/multica?sslmode=disable";

const TOTAL_EVENTS = 10_000;
const PAGE_SIZE = 50; // matches executionLogPageOptions default

let api: TestApiClient;
let db: pg.Client;
let issueId: string;
let slug: string;
let taskId: string;
let agentId: string;
let runtimeId: string;

test.beforeAll(async () => {
  api = await createTestApi();
  const issue = await api.createIssue("Execution log 10k render");
  issueId = issue.id;

  db = new pg.Client(DATABASE_URL);
  await db.connect();

  const { rows: wsRows } = await db.query(
    `SELECT w.id AS workspace_id, w.slug AS slug
       FROM issue i JOIN workspace w ON w.id = i.workspace_id
      WHERE i.id = $1`,
    [issueId],
  );
  const workspaceId = wsRows[0].workspace_id as string;
  slug = wsRows[0].slug as string;

  runtimeId = (
    await db.query(
      `INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at)
       VALUES ($1, NULL, 'Exec Log E2E Runtime', 'cloud', 'e2e', 'online', 'e2e', '{}'::jsonb, now())
       RETURNING id`,
      [workspaceId],
    )
  ).rows[0].id;

  agentId = (
    await db.query(
      `INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks)
       VALUES ($1, 'Exec Log E2E Agent', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1)
       RETURNING id`,
      [workspaceId, runtimeId],
    )
  ).rows[0].id;

  taskId = (
    await db.query(
      `INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id, completed_at)
       VALUES ($1, $2, 'completed', 0, $3, now())
       RETURNING id`,
      [agentId, runtimeId, issueId],
    )
  ).rows[0].id;

  // 10k persisted events in one statement. Cycle the five known types; the text /
  // thinking / error rows carry "event <seq>" so a row can be located by seq.
  await db.query(
    `INSERT INTO task_message (task_id, seq, type, tool, content, created_at)
     SELECT $1, g,
            (ARRAY['text','thinking','tool_use','tool_result','error'])[1 + (g % 5)],
            CASE WHEN g % 5 IN (2, 3) THEN 'exec_command' ELSE NULL END,
            'event ' || g,
            now() - (interval '1 second' * ($2 - g))
       FROM generate_series(1, $2) g`,
    [taskId, TOTAL_EVENTS],
  );
});

test.afterAll(async () => {
  if (db) {
    // task_message cascades on the task delete (migration 026).
    if (taskId) await db.query(`DELETE FROM agent_task_queue WHERE id = $1`, [taskId]);
    if (agentId) await db.query(`DELETE FROM agent WHERE id = $1`, [agentId]);
    if (runtimeId) await db.query(`DELETE FROM agent_runtime WHERE id = $1`, [runtimeId]);
    await db.end();
  }
  if (api) await api.cleanup();
});

async function openExecutionLog(page: import("@playwright/test").Page) {
  await loginAsDefault(page);
  await page.goto(`/${slug}/issues/${issueId}`, { waitUntil: "domcontentloaded" });

  // Past runs are folded behind a toggle; expand, then open the transcript.
  const showPast = page.getByTestId("show-past-runs");
  await expect(showPast).toBeVisible({ timeout: 30000 });
  await showPast.click();

  // The row's action buttons are revealed on hover (hover-capable pointers);
  // hover the past run row so its transcript button becomes clickable.
  const row = page.locator('[class*="group/execution-log-row"]').first();
  await expect(row).toBeVisible({ timeout: 10000 });
  await row.hover();
  await page.getByTestId("transcript-button").first().click();

  const dialog = page.getByTestId("execution-log-dialog");
  await expect(dialog).toBeVisible({ timeout: 15000 });
  // Wait until the first page has resolved (total populated).
  await expect(page.getByTestId("execution-log-total")).toHaveText(String(TOTAL_EVENTS), {
    timeout: 15000,
  });
  return dialog;
}

test("a 10k-event Run renders a bounded number of DOM rows", async ({ page }) => {
  await openExecutionLog(page);

  const rows = page.getByTestId("execution-log-row");
  await expect(rows.first()).toBeVisible({ timeout: 15000 });

  // The whole Run is 10,000 events; virtualization must keep the mounted rows
  // to roughly the viewport + overscan — orders of magnitude below the total.
  const mounted = await rows.count();
  expect(mounted).toBeGreaterThan(0);
  expect(mounted).toBeLessThan(200);

  // And only the newest page's worth of events is loaded, not the whole array.
  await expect(page.getByTestId("execution-log-dialog")).toContainText(`${PAGE_SIZE} loaded`);
});

test("loading older history keeps the reading anchor in place", async ({ page }) => {
  await openExecutionLog(page);
  const dialog = page.getByTestId("execution-log-dialog");
  const scroll = page.getByTestId("execution-log-scroll");
  await expect(dialog).toContainText(`${PAGE_SIZE} loaded`);

  // First page is the newest 50 events (seq 9951..10000); its top ("fold") row
  // is seq 9951. Scroll up with real wheel events (Virtuoso owns its scroll, so
  // a programmatic scrollTop is not honoured) until reaching the top triggers
  // the older page — seq 9901..9950 prepends → 100 loaded.
  const anchor = page.getByTestId("execution-log-row").filter({ hasText: "#9951" });
  await scroll.hover();
  await expect(async () => {
    await page.mouse.wheel(0, -3000);
    await expect(dialog).toContainText(`${PAGE_SIZE * 2} loaded`, { timeout: 800 });
  }).toPass({ timeout: 25000 });

  await page.waitForTimeout(400); // let the virtualizer settle the prepend

  // Anchor preserved: the fold row (seq 9951) is still on screen after older
  // history prepended above it — the virtualizer shifted the viewport down to
  // hold its position instead of snapping to the newly prepended top (which
  // would leave scrollTop at 0 and jump the reader away).
  await expect(anchor).toBeInViewport();
  expect(await scroll.evaluate((el) => el.scrollTop)).toBeGreaterThan(0);
});
