// FIR-3901 — screen evidence for the red failed-run bar and the matching inbox
// pip. Seeds one real dead failed run (failed, settled past the grace window,
// no retry descendant, resumable session on an online runtime) and drives the
// two surfaces the feature adds: the bar at the top of the issue, collapsed and
// expanded, and the red pip on the issue's inbox row.
//
// Single-purpose and not part of the regular CI suite; run via
// `pnpm exec playwright test e2e/fir-3901-failed-run-bar-screenshots.spec.ts --project=chromium`.

import "./env";
import { expect, test } from "@playwright/test";
import { mkdirSync } from "node:fs";
import pg from "pg";
import { loginAsDefault } from "./helpers";
import { TestApiClient } from "./fixtures";

const DATABASE_URL =
  process.env.DATABASE_URL ??
  "postgres://multica:multica@localhost:5432/multica?sslmode=disable";

const SHOT_DIR = "e2e/screenshots/fir-3901";

test.beforeAll(() => {
  mkdirSync(SHOT_DIR, { recursive: true });
});

/**
 * A realistic dead run: the agent works, then the process dies mid-step. The
 * error lands in the last tool result, which is what the expanded card shows.
 */
const FAILED_MESSAGES: Array<{
  type: string;
  tool?: string;
  content?: string;
  input?: Record<string, unknown>;
  output?: string;
}> = [
  {
    type: "text",
    content:
      "Picking this up. I will map the failing runs first, then fix the retry path so a dead run stops being invisible.",
  },
  {
    type: "thinking",
    content:
      "The failure reason is dropped when the agent process exits without a result, so the run is classified as unknown and never retried.",
  },
  {
    type: "tool_use",
    tool: "Bash",
    input: { command: "go test ./server/internal/cerebro/inbox/..." },
  },
  {
    type: "tool_result",
    tool: "Bash",
    output:
      "ok  \tgithub.com/multica-ai/multica/server/internal/cerebro/inbox\t0.412s",
  },
  {
    type: "text",
    content: "Tests pass. Writing the retry classification next.",
  },
  {
    type: "error",
    content: "claude exited with signal SIGKILL after 26 minutes",
  },
];

test("FIR-3901 — red failed-run bar on the issue and the red pip in the inbox", async ({
  page,
}) => {
  test.setTimeout(240_000);
  await page.setViewportSize({ width: 1440, height: 1000 });

  const slug = await loginAsDefault(page);
  const api = new TestApiClient();
  await api.login("e2e@multica.ai", "E2E User");
  const workspace = (await api.getWorkspaces())[0]!;
  api.setWorkspaceId(workspace.id);
  api.setWorkspaceSlug(workspace.slug);

  const database = new pg.Client(DATABASE_URL);
  await database.connect();

  const suffix = Date.now().toString(36);
  const userId = (
    await database.query(`SELECT id FROM "user" WHERE email = $1 LIMIT 1`, [
      "e2e@multica.ai",
    ])
  ).rows[0].id as string;

  // The runtime must be ONLINE, otherwise resume_possible is false and the
  // Resume button renders disabled — a different screen than the one under test.
  const runtimeId = (
    await database.query(
      `INSERT INTO agent_runtime (
         workspace_id, name, runtime_mode, provider, status, device_info,
         metadata, last_seen_at
       ) VALUES ($1, $2, 'cloud', 'firtal-gateway', 'online', $2, '{}'::jsonb, now())
       RETURNING id`,
      [workspace.id, `FIR-3901 runtime ${suffix}`],
    )
  ).rows[0].id as string;
  const agentId = (
    await database.query(
      `INSERT INTO agent (
         workspace_id, name, runtime_mode, runtime_config, runtime_id,
         visibility, max_concurrent_tasks, owner_id
       ) VALUES ($1, $2, 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
       RETURNING id`,
      [workspace.id, `FIR-3901 Agent ${suffix}`, runtimeId, userId],
    )
  ).rows[0].id as string;

  const issue = await api.createIssue(`FIR-3901 dead failed run ${suffix}`);
  let inboxItemId: string | null = null;

  try {
    // ── Seed the dead failed run ──────────────────────────────────────────
    // completed_at is 5 minutes ago: past DeadFailedGraceSeconds (60) and well
    // inside DeadFailedWindowHours (48). session_id is set, so the daemon could
    // pick the conversation back up — resume_possible must come back true.
    const taskId = (
      await database.query(
        `INSERT INTO agent_task_queue (
           agent_id, issue_id, runtime_id, status, priority, initiator_user_id,
           failure_reason, error, trigger_summary, session_id,
           attempt, max_attempts, started_at, completed_at
         ) VALUES ($1, $2, $3, 'failed', 0, $4,
                   'agent_error.process_failure',
                   'claude exited with signal SIGKILL after 26 minutes',
                   $5, $6, 2, 2,
                   now() - interval '31 minutes', now() - interval '5 minutes')
         RETURNING id`,
        [
          agentId,
          issue.id,
          runtimeId,
          userId,
          "Map every failing run in the last 24 hours and fix the retry path.",
          `sess-fir3901-${suffix}`,
        ],
      )
    ).rows[0].id as string;

    for (const [index, message] of FAILED_MESSAGES.entries()) {
      await database.query(
        `INSERT INTO task_message (task_id, seq, type, tool, content, input, output, created_at)
         VALUES ($1, $2::int, $3, $4, $5, $6, $7,
                 now() - make_interval(mins => 31 - $2::int))`,
        [
          taskId,
          index + 1,
          message.type,
          message.tool ?? null,
          message.content ?? null,
          message.input ? JSON.stringify(message.input) : null,
          message.output ?? null,
        ],
      );
    }

    // The inbox row the pip attaches to. The pip is derived from the dead-run
    // feed, not stored on the item, so an ordinary info item is the right seed.
    inboxItemId = (
      await database.query(
        `INSERT INTO inbox_item (
           workspace_id, recipient_type, recipient_id, type, severity,
           issue_id, title, route, read, archived, created_at
         ) VALUES ($1, 'member', $2, 'new_comment', 'attention', $3, $4,
                   'inbox', false, false, now() - interval '4 minutes')
         RETURNING id`,
        [
          workspace.id,
          userId,
          issue.id,
          `FIR-3901 dead failed run ${suffix}`,
        ],
      )
    ).rows[0].id as string;

    // ── 1. The bar, collapsed, at the top of the issue ────────────────────
    await page.goto(`/${slug}/issues/${issue.id}`, {
      waitUntil: "domcontentloaded",
    });

    // The bar only renders when the dead-run feed returns a row, and Resume is
    // only enabled when that row says resume_possible — so these UI assertions
    // are the API contract observed through the surface the user actually sees.
    const bar = page.getByTestId("failed-run-bar");
    await expect(bar).toBeVisible({ timeout: 30_000 });
    await expect(bar).toContainText("The agent process crashed");
    await expect(bar).toHaveAttribute("aria-expanded", "false");
    await page.screenshot({ path: `${SHOT_DIR}/1-bar-collapsed.png` });

    // ── 2. The bar, expanded: why it died + the two ways forward ──────────
    await bar.click();
    await expect(bar).toHaveAttribute("aria-expanded", "true");
    const resume = page.getByRole("button", { name: "Resume" });
    const startOver = page.getByRole("button", { name: "Start over" });
    await expect(resume).toBeVisible();
    await expect(resume).toBeEnabled();
    await expect(startOver).toBeVisible();
    await expect(startOver).toBeEnabled();
    await expect(page.getByText(/SIGKILL/).first()).toBeVisible();
    await page.screenshot({ path: `${SHOT_DIR}/2-bar-expanded.png` });

    // ── 3. The red pip on the issue's inbox row ───────────────────────────
    await page.goto(`/${slug}/inbox`, { waitUntil: "domcontentloaded" });
    // Resumable runs carry the longer title, so match on the prefix.
    const pip = page.locator('[title^="Run failed"]').first();
    await expect(pip).toBeVisible({ timeout: 30_000 });
    await expect(pip).toHaveAttribute("title", "Run failed — can be continued");
    // The dot itself carries the colour; the wrapper only positions it.
    await expect(pip.locator(".bg-destructive").first()).toBeVisible();
    await page.screenshot({ path: `${SHOT_DIR}/3-inbox-red-pip.png` });
    // Close-up: the dot is 8px wide, so the full-page shot alone does not
    // carry the evidence on a phone or in a comment thumbnail.
    await page.screenshot({
      path: `${SHOT_DIR}/4-inbox-red-pip-closeup.png`,
      clip: { x: 257, y: 55, width: 330, height: 165 },
    });
  } finally {
    if (inboxItemId) {
      await database.query(`DELETE FROM inbox_item WHERE id = $1`, [inboxItemId]);
    }
    await database.query(`DELETE FROM agent_task_queue WHERE agent_id = $1`, [
      agentId,
    ]);
    await database.query(`DELETE FROM agent WHERE id = $1`, [agentId]);
    await database.query(`DELETE FROM agent_runtime WHERE id = $1`, [runtimeId]);
    await database.end();
    await api.cleanup();
  }
});
