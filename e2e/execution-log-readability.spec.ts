import { test, expect, type Page } from "@playwright/test";
import pg from "pg";
import { createTestApi, loginAsDefault } from "./helpers";
import type { TestApiClient } from "./fixtures";

// MUL-5122 — product acceptance for the Execution Log reading hierarchy and the
// restored controls (sort preference + expand/collapse of loaded items). Uses a
// small REALISTIC mixed fixture (long Agent text, thinking, tool calls, a long
// tool result, an error) so the assertions and the captured screenshots reflect
// real reading, not a synthetic 10k stress run. Screenshots go to
// SHOTS_DIR for manual/product review; the assertions are the regression gate.

const DATABASE_URL =
  process.env.DATABASE_URL ??
  "postgres://multica:multica@localhost:5432/multica?sslmode=disable";
const SHOTS_DIR = process.env.EXEC_LOG_SHOTS_DIR ?? "/tmp/mul5122_shots";

const LONG_AGENT = [
  "Here is the full plan for the Execution Log optimization.",
  "",
  "First, the server gains an additive paged endpoint that returns a bounded",
  "window of events with stable (seq, id) cursors, full-Run totals, and dynamic",
  "type/tool facets. The legacy array endpoint stays untouched for Chat, Mobile",
  "and the CLI.",
  "",
  "On the web, the terminal transcript opens immediately and loads only the",
  "newest page; older history streams in as you scroll, the list is virtualized",
  "so a 10,000-event run stays bounded, and the reading hierarchy makes the",
  "agent's narration the primary layer while tool calls and thinking stay",
  "compact and expandable.",
  "",
  "Next steps: wire the presenter rules, restore sort and bulk expand, and take",
  "browser screenshots against a realistic mixed fixture.",
].join("\n");

const LONG_RESULT = Array.from({ length: 40 }, (_, i) => `server/internal/handler/daemon.go:${3500 + i}: match ${i}`).join("\n");

interface Ev {
  type: string;
  tool?: string;
  content?: string;
  input?: Record<string, unknown>;
  output?: string;
}

const EVENTS: Ev[] = [
  { type: "text", content: "Starting the task — let me look at the current Execution Log implementation." },
  { type: "thinking", content: "The user reports the dialog freezes on long runs. Likely the whole message array is fetched and every row is mounted. I should confirm in the handler and the web component before choosing an approach." },
  { type: "tool_use", tool: "Bash", input: { command: "grep -rn 'ListTaskMessages' server/internal/handler" } },
  { type: "tool_result", tool: "Bash", output: LONG_RESULT },
  { type: "tool_use", tool: "Read", input: { file_path: "/server/internal/handler/daemon.go" } },
  { type: "tool_result", tool: "Read", output: "func (h *Handler) ListTaskMessagesByUser(w http.ResponseWriter, r *http.Request) {\n  // returns the full array — the freeze source\n}" },
  { type: "error", content: "Error: failed to connect to database: connection refused\n  at db.Connect (db.go:42)\n  at main (server.go:88)" },
  { type: "tool_use", tool: "exec_command", input: { command: "pnpm --filter @multica/views test" } },
  { type: "tool_result", tool: "exec_command", output: "Test Files  6 passed (6)\n     Tests  51 passed (51)" },
  { type: "thinking", content: "Pagination plus virtualization fixes the freeze; the presenter needs to drive the reading hierarchy so agent text is the primary layer." },
  { type: "text", content: LONG_AGENT },
];

let api: TestApiClient;
let db: pg.Client;
let issueId: string;
let slug: string;
let taskId: string;
let agentId: string;
let runtimeId: string;

test.beforeAll(async () => {
  api = await createTestApi();
  const issue = await api.createIssue("Execution log readability");
  issueId = issue.id;

  db = new pg.Client(DATABASE_URL);
  await db.connect();

  const { rows: wsRows } = await db.query(
    `SELECT w.id AS workspace_id, w.slug AS slug FROM issue i JOIN workspace w ON w.id = i.workspace_id WHERE i.id = $1`,
    [issueId],
  );
  const workspaceId = wsRows[0].workspace_id as string;
  slug = wsRows[0].slug as string;

  runtimeId = (
    await db.query(
      `INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at)
       VALUES ($1, NULL, 'Readability E2E Runtime', 'cloud', 'e2e', 'online', 'e2e', '{}'::jsonb, now()) RETURNING id`,
      [workspaceId],
    )
  ).rows[0].id;
  agentId = (
    await db.query(
      `INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks)
       VALUES ($1, 'Codex', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1) RETURNING id`,
      [workspaceId, runtimeId],
    )
  ).rows[0].id;
  taskId = (
    await db.query(
      `INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id, completed_at)
       VALUES ($1, $2, 'completed', 0, $3, now()) RETURNING id`,
      [agentId, runtimeId, issueId],
    )
  ).rows[0].id;

  for (let i = 0; i < EVENTS.length; i++) {
    const e = EVENTS[i]!;
    await db.query(
      `INSERT INTO task_message (task_id, seq, type, tool, content, input, output, created_at)
       VALUES ($1, $2, $3, $4, $5, $6, $7, now() - (interval '1 second' * ($8::int - $2::int)))`,
      [taskId, i + 1, e.type, e.tool ?? null, e.content ?? null, e.input ? JSON.stringify(e.input) : null, e.output ?? null, EVENTS.length],
    );
  }
});

test.afterAll(async () => {
  if (db) {
    if (taskId) await db.query(`DELETE FROM agent_task_queue WHERE id = $1`, [taskId]);
    if (agentId) await db.query(`DELETE FROM agent WHERE id = $1`, [agentId]);
    if (runtimeId) await db.query(`DELETE FROM agent_runtime WHERE id = $1`, [runtimeId]);
    await db.end();
  }
  if (api) await api.cleanup();
});

async function openLog(page: Page) {
  await loginAsDefault(page);
  await page.goto(`/${slug}/issues/${issueId}`, { waitUntil: "domcontentloaded" });
  const showPast = page.getByTestId("show-past-runs");
  await expect(showPast).toBeVisible({ timeout: 30000 });
  await showPast.click();
  const row = page.locator('[class*="group/execution-log-row"]').first();
  await expect(row).toBeVisible({ timeout: 10000 });
  await row.hover();
  await page.getByTestId("transcript-button").first().click();
  const dialog = page.getByTestId("execution-log-dialog");
  await expect(dialog).toBeVisible({ timeout: 15000 });
  await expect(page.getByTestId("execution-log-total")).toHaveText(String(EVENTS.length), { timeout: 15000 });
  await expect(page.getByTestId("execution-log-row").first()).toBeVisible();
  return dialog;
}

test("reading hierarchy: agent text is a readable multi-line body with expand", async ({ page }) => {
  const dialog = await openLog(page);
  await page.waitForTimeout(500);
  await dialog.screenshot({ path: `${SHOTS_DIR}/01-default-chronological.png` });

  // The long Agent conclusion renders as a real multi-line body (tall), not a
  // single truncated line, and offers a "Show more" affordance.
  const agentRow = page.getByTestId("execution-log-row").filter({ hasText: "full plan for the Execution Log" });
  await expect(agentRow).toBeVisible();
  const box = await agentRow.boundingBox();
  expect(box!.height).toBeGreaterThan(60); // several lines tall, not one line
  await expect(agentRow.getByRole("button", { name: /show more/i })).toBeVisible();
});

test("sort preference: toggling newest-first reverses the on-screen order", async ({ page }) => {
  await openLog(page);
  const rows = page.getByTestId("execution-log-row");
  const sort = page.getByTestId("execution-log-sort");

  // Chronological (default): scroll to the very top shows the oldest (#1).
  await page.getByTestId("execution-log-scroll").evaluate((el) => (el.scrollTop = 0));
  await expect(rows.first()).toContainText("#1");

  // Switch to newest-first: the first row becomes the newest (#11).
  await sort.getByRole("button", { name: /newest/i }).click();
  await page.getByTestId("execution-log-scroll").evaluate((el) => (el.scrollTop = 0));
  await expect(rows.first()).toContainText(`#${EVENTS.length}`);
  await page.waitForTimeout(400);
  await page.getByTestId("execution-log-dialog").screenshot({ path: `${SHOTS_DIR}/02-newest-first.png` });

  // Restore for other tests (preference persists in the store).
  await sort.getByRole("button", { name: /oldest/i }).click();
});

test("expand: single row and bulk expand of loaded items", async ({ page }) => {
  const dialog = await openLog(page);

  // Single expand: click a tool_result row → its full monospace <pre> appears.
  const resultRow = page.getByTestId("execution-log-row").filter({ hasText: "match 0" }).first();
  await resultRow.scrollIntoViewIfNeeded();
  await expect(resultRow.locator("pre")).toHaveCount(1); // 2-line preview
  await resultRow.getByRole("button").first().click();
  await expect(resultRow.getByText("match 39")).toBeVisible(); // full output now shown
  await page.waitForTimeout(300);
  await dialog.screenshot({ path: `${SHOTS_DIR}/03-single-expanded.png` });

  // Bulk expand loaded: every expandable loaded row opens.
  const preCountBefore = await page.locator('[data-testid="execution-log-row"] pre').count();
  await page.getByTestId("execution-log-expand-all").click();
  await expect(page.getByTestId("execution-log-collapse-all")).toBeVisible();
  await page.waitForTimeout(400);
  const preCountAfter = await page.locator('[data-testid="execution-log-row"] pre').count();
  expect(preCountAfter).toBeGreaterThan(preCountBefore);
  await dialog.screenshot({ path: `${SHOTS_DIR}/04-bulk-expanded.png` });

  // Collapse all returns to previews.
  await page.getByTestId("execution-log-collapse-all").click();
  await expect(page.getByTestId("execution-log-expand-all")).toBeVisible();
});
