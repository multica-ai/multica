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
  // Historical double-JSON-encoded tool result (Claude/CodeBuddy) exactly like the
  // reported screenshot: the stored output is a JSON string literal with outer
  // quotes, escaped \n and \". The frontend must decode ONE level → readable text.
  { type: "tool_result", tool: "Bash", output: '"Comment added to issue PRE-3.\\n{\\n  \\"attachments\\": [],\\n  \\"status\\": \\"in_review\\"\\n}"' },
  // A raw JSON OBJECT tool result: the collapsed summary must read its key fields
  // (identifier/title/status), never a bare `{`; expand shows the pretty JSON.
  {
    type: "tool_result",
    tool: "create_issue",
    output: JSON.stringify({
      identifier: "MUL-9001",
      title: "Paged Execution Log window",
      status: "in_review",
      url: "https://multica.test/MUL-9001",
    }),
  },
  // A raw JSON ARRAY tool result: summarized as its count + first element fields.
  {
    type: "tool_result",
    tool: "list_issues",
    output: JSON.stringify([
      { identifier: "MUL-9001", status: "in_review" },
      { identifier: "MUL-9002", status: "todo" },
    ]),
  },
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

  // Chronological opens anchored at the newest event; scroll to the top so the
  // early tool_result row (seq 4) is mounted before we locate it.
  await page.getByTestId("execution-log-scroll").evaluate((el) => (el.scrollTop = 0));
  await page.waitForTimeout(300);

  // Single expand: click a tool_result row → its full monospace <pre> appears.
  const resultRow = page.getByTestId("execution-log-row").filter({ hasText: "match 0" }).first();
  await resultRow.scrollIntoViewIfNeeded();
  await expect(resultRow.locator("pre")).toHaveCount(1); // 2-line preview
  await resultRow.getByRole("button").first().click();
  await expect(resultRow.getByText("match 39")).toBeVisible(); // full output now shown
  await page.waitForTimeout(300);
  await dialog.screenshot({ path: `${SHOTS_DIR}/03-single-expanded.png` });

  // Bulk expand loaded: every expandable loaded row opens. The main label is the
  // fixed "Expand all / Collapse all" (scope lives in the tooltip), not "Expand
  // loaded" — the ambiguity the product owner flagged.
  const expandBtn = page.getByTestId("execution-log-expand-all");
  await expect(expandBtn).toContainText("Expand all");
  await expect(expandBtn).not.toContainText("Expand loaded");
  const preCountBefore = await page.locator('[data-testid="execution-log-row"] pre').count();
  await expandBtn.click();
  await expect(page.getByTestId("execution-log-collapse-all")).toBeVisible();
  await expect(page.getByTestId("execution-log-collapse-all")).toContainText("Collapse all");
  await page.waitForTimeout(400);
  const preCountAfter = await page.locator('[data-testid="execution-log-row"] pre').count();
  expect(preCountAfter).toBeGreaterThan(preCountBefore);
  await dialog.screenshot({ path: `${SHOTS_DIR}/04-bulk-expanded.png` });

  // Collapse all returns to previews.
  await page.getByTestId("execution-log-collapse-all").click();
  await expect(page.getByTestId("execution-log-expand-all")).toBeVisible();
});

test("compat: a double-encoded tool result renders as readable decoded text", async ({ page }) => {
  const dialog = await openLog(page);

  // The stored output is a JSON-encoded string (outer quotes, escaped \n and \").
  // It must render decoded — real newlines/quotes — not the escaped blob from the
  // reported screenshot. The collapsed preview already shows the decoded first line.
  const row = page
    .getByTestId("execution-log-row")
    .filter({ hasText: "Comment added to issue PRE-3" })
    .first();
  await row.scrollIntoViewIfNeeded();
  await expect(row).toBeVisible();
  await row.getByRole("button").first().click();

  // Decoded: the fields read as real text, and no escaped `\n` literal survives.
  await expect(row).toContainText('"status": "in_review"');
  await expect(row).not.toContainText("\\n");
  await page.waitForTimeout(300);
  await dialog.screenshot({ path: `${SHOTS_DIR}/05-decoded-tool-result.png` });
});

test("filter: the popover narrows the list to a facet and clears back", async ({ page }) => {
  await openLog(page);
  const filter = page.getByTestId("execution-log-filter");
  const timeline = page.getByTestId("execution-log-timeline");
  const agentRow = page
    .getByTestId("execution-log-row")
    .filter({ hasText: "full plan for the Execution Log" });

  // No filter yet: the trigger carries no selected-count badge.
  await expect(filter).not.toContainText("1");

  // Open the popover — facet chips (aria-pressed toggles) live in the portaled
  // content, so screenshot the whole page to capture the overlay.
  await filter.click();
  const errorChip = page.locator("button[aria-pressed]").filter({ hasText: /error/i }).first();
  await expect(errorChip).toBeVisible();
  await page.waitForTimeout(200);
  await page.screenshot({ path: `${SHOTS_DIR}/06-filter-popover.png` });

  // Selecting the Error facet drops every non-error row from the data (not just
  // scrolls it away), so the Agent narration is gone and the counts show "matched".
  await errorChip.click();
  await expect(filter).toContainText("1");
  await expect(timeline).toContainText("matched");
  await expect(agentRow).toHaveCount(0);
  await expect(
    page.getByTestId("execution-log-row").filter({ hasText: /connection refused/i }).first(),
  ).toBeVisible();

  // Clear restores the full run at the data level. Selecting a chip dismisses the
  // popover, so re-open it if the Clear control isn't already on screen.
  const clear = page.getByRole("button", { name: "Clear" });
  if (!(await clear.isVisible().catch(() => false))) {
    await filter.click();
    await expect(clear).toBeVisible();
  }
  await clear.click();
  await expect(filter).not.toContainText("1");
  await expect(timeline).not.toContainText("matched");
});

test("default-expand: the 'more' menu toggle opens all loaded content", async ({ page }) => {
  const dialog = await openLog(page);

  // Open the "more" menu: it holds copy, the default-expand preference, and the
  // run-detail rows (runtime + mode) that were moved out of the flat header.
  await page.getByTestId("execution-log-more").click();
  await expect(page.getByText("Copy loaded")).toBeVisible();
  await expect(page.getByText(/Readability E2E Runtime \(cloud\)/)).toBeVisible();
  const toggle = page.getByTestId("execution-log-default-expand");
  await expect(toggle).toBeVisible();
  await page.waitForTimeout(200);
  await page.screenshot({ path: `${SHOTS_DIR}/07-more-menu.png` });

  // Turning it on expands every loaded expandable row — the toolbar's expand
  // control flips to "collapse all", the same end state as bulk-expand.
  await toggle.click();
  await page.keyboard.press("Escape"); // dismiss the menu to view the list
  await expect(page.getByTestId("execution-log-collapse-all")).toBeVisible();
  await page.waitForTimeout(300);
  await dialog.screenshot({ path: `${SHOTS_DIR}/08-default-expanded.png` });
});

test("timeline: clicking a color segment locates that event in the list", async ({ page }) => {
  const dialog = await openLog(page);
  const timeline = page.getByTestId("execution-log-timeline");
  await expect(timeline).toBeVisible();

  // Chronological opens anchored at the newest event; the oldest (#1, the
  // "Starting the task" text) is off-screen at the top.
  const firstEvent = page
    .getByTestId("execution-log-row")
    .filter({ hasText: "Starting the task" })
    .first();

  // One clickable segment per loaded event, in order. Clicking the first jumps
  // the virtualized list to event #1.
  const firstSegment = timeline.locator('[role="navigation"] button').first();
  await firstSegment.click();
  await expect(firstEvent).toBeVisible({ timeout: 5000 });
  await page.waitForTimeout(300);
  await dialog.screenshot({ path: `${SHOTS_DIR}/09-timeline-locate.png` });
});

test("json result: key-field summary, formatted expansion, and single-row copy", async ({
  page,
  context,
}) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  const dialog = await openLog(page);

  // "Paged Execution Log window" (the object's title field) is unique to the
  // JSON-object result row — the array row has no title.
  const jsonRow = page
    .getByTestId("execution-log-row")
    .filter({ hasText: "Paged Execution Log window" })
    .first();
  await jsonRow.scrollIntoViewIfNeeded();

  // Collapsed: the summary reads the object's key fields — a bare-`{` first line
  // could never surface "status: in_review".
  await expect(jsonRow).toContainText("identifier: MUL-9001");
  await expect(jsonRow).toContainText("status: in_review");
  await jsonRow.hover(); // reveal the per-row copy affordance for the screenshot
  await page.waitForTimeout(200);
  await dialog.screenshot({ path: `${SHOTS_DIR}/10-json-summary-collapsed.png` });

  // Expand (the label toggle is the row's first button): the body is the
  // pretty-printed JSON with quoted keys on their own indented lines.
  await jsonRow.getByRole("button").first().click();
  const pre = jsonRow.locator("pre");
  await expect(pre).toContainText('"identifier": "MUL-9001"');
  await expect(pre).toContainText('"status": "in_review"');
  await page.waitForTimeout(200);
  await dialog.screenshot({ path: `${SHOTS_DIR}/11-json-expanded.png` });

  // Single-row copy: the per-row button puts the decoded/pretty body on the
  // clipboard (revealed on hover, then read back through the Clipboard API).
  await jsonRow.hover();
  await jsonRow.getByTestId("execution-log-row-copy").click();
  await expect(jsonRow.getByTestId("execution-log-row-copy")).toBeVisible();
  const clip = await page.evaluate(() => navigator.clipboard.readText());
  expect(clip).toContain('"identifier": "MUL-9001"');
  expect(clip).toContain('"status": "in_review"');

  // A JSON ARRAY result summarizes as its count + first element's fields.
  const arrayRow = page
    .getByTestId("execution-log-row")
    .filter({ hasText: "[2]" })
    .filter({ hasText: "MUL-9001" })
    .first();
  await arrayRow.scrollIntoViewIfNeeded();
  await expect(arrayRow).toContainText("[2]");
});
