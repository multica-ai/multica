// FIR-3782 Phase 4 — screen evidence for the ported execution-log revamp.
// Seeds two real runs (one completed, one failed) on real issues, then drives
// the Logs dialog through every mode the port changed: the Focus default, the
// Expand all and Collapse all modes, the failure card on a failed run, and the
// two fork-only disclosures the port originally dropped.
//
// Single-purpose and not part of the regular CI suite; run via
// `pnpm exec playwright test e2e/fir-3782-transcript-screenshots.spec.ts`.

import "./env";
import { expect, test, type Page } from "@playwright/test";
import { mkdirSync } from "node:fs";
import pg from "pg";
import { loginAsDefault } from "./helpers";
import { TestApiClient } from "./fixtures";

const DATABASE_URL =
  process.env.DATABASE_URL ??
  "postgres://multica:multica@localhost:5432/multica?sslmode=disable";

const SHOT_DIR = "e2e/screenshots/fir-3782";

test.beforeAll(() => {
  mkdirSync(SHOT_DIR, { recursive: true });
});

/** One realistic completed run: agent replies, thinking, and tool steps. */
const COMPLETED_MESSAGES: Array<{
  type: string;
  tool?: string;
  content?: string;
  input?: Record<string, unknown>;
  output?: string;
}> = [
  {
    type: "text",
    content:
      "Picking this up. The redirect drops the workspace slug after login, so I will start at the auth callback and work forward from there.",
  },
  {
    type: "thinking",
    content:
      "The slug is read from localStorage before the token is written, so on a cold login it is still empty. Either write the token first or read the slug from the callback payload.",
  },
  {
    type: "tool_use",
    tool: "Grep",
    input: { pattern: "multica_token", path: "packages/views/auth" },
  },
  {
    type: "tool_result",
    tool: "Grep",
    output:
      "packages/views/auth/callback.tsx:41:  localStorage.setItem(\"multica_token\", token);\npackages/views/auth/callback.tsx:44:  const slug = localStorage.getItem(\"workspace_slug\");",
  },
  {
    type: "text",
    content:
      "Found it. The slug is read on line 44 but only written once the workspace query resolves, which is after the redirect fires.",
  },
  {
    type: "tool_use",
    tool: "Bash",
    input: { command: "pnpm test --filter @multica/views auth" },
  },
  {
    type: "tool_result",
    tool: "Bash",
    output:
      "Test Files  4 passed (4)\n     Tests  31 passed (31)\n  Duration  6.42s",
  },
  {
    type: "text",
    content:
      "Fixed: the redirect now waits for the workspace query instead of reading a slug that is not there yet. All 31 auth tests pass.",
  },
];

/** One realistic failed run: the error lands in the last tool result. */
const FAILED_MESSAGES: typeof COMPLETED_MESSAGES = [
  {
    type: "text",
    content:
      "Running the migration against the staging database before I touch anything else.",
  },
  {
    type: "thinking",
    content:
      "The migration adds a not-null column, so it needs a default or the backfill has to run first.",
  },
  {
    type: "tool_use",
    tool: "Bash",
    input: { command: "cd server && go run ./cmd/migrate up" },
  },
  {
    type: "tool_result",
    tool: "Bash",
    output:
      "  up  9154_cerebro_commands\n  up  9155_cerebro_apps_group_capabilities\nERROR: column \"role_id\" of relation \"cerebro_role_member\" contains null values (SQLSTATE 23502)\nmigration 9156_cerebro_service_token failed and was rolled back",
  },
  {
    type: "error",
    content:
      "migrate: column \"role_id\" of relation \"cerebro_role_member\" contains null values (SQLSTATE 23502)",
  },
];

/**
 * What the daemon records as actually delivered to the model: the runtime
 * brief written into the workdir, then the task prompt sent as the user turn.
 * The triggering comment is a fraction of the last layer — which is the whole
 * point of showing this instead of `trigger_summary`.
 */
const PROMPT_LAYERS = [
  {
    name: "runtime_brief",
    delivery: "workdir_file",
    byte_size: 2140,
    sha256_original: "a1",
    sha256_redacted: "a1",
    content_redacted: [
      "# Multica Agent Runtime",
      "",
      "You are a coding agent in the Multica platform. Use the `multica` CLI to",
      "interact with the platform.",
      "",
      "## Agent Identity",
      "",
      "You are: Mia (ID: 4d8b4a77-e0df-4d5d-b279-a13587e8ff74)",
      "",
      "## Requesting User",
      "",
      "You are working on behalf of Jesper Hvejsel.",
      "",
      "## Credentials",
      "",
      "GITHUB_TOKEN=[REDACTED]",
      "",
      "## Available Commands",
      "",
      "- multica issue get <id> --output json — Get full issue details.",
      "- multica issue comment add <issue-id> --content-stdin — Post a comment.",
      "- multica artifact create --kind plan --title ... — Create a document.",
    ].join("\n"),
  },
  {
    name: "task_prompt",
    delivery: "user_prompt",
    byte_size: 612,
    sha256_original: "b2",
    sha256_redacted: "b2",
    content_redacted: [
      "Your assigned issue ID is: 73f3f200-0ccb-418a-85b3-22e7c6f2ed27",
      "",
      "[NEW COMMENT] A user just left a new comment:",
      "",
      "> Fix the login redirect so it keeps the workspace slug. Reproduce first,",
      "> then fix, then run the auth tests.",
      "",
      "Start by running `multica issue get ... --output json` to understand the",
      "issue context, then decide how to proceed.",
    ].join("\n"),
  },
];

type PromptLayer = (typeof PROMPT_LAYERS)[number];

async function seedRun(
  database: pg.Client,
  args: {
    agentId: string;
    issueId: string;
    runtimeId: string;
    userId: string;
    status: "completed" | "failed";
    failureReason: string | null;
    triggerSummary: string;
    messages: typeof COMPLETED_MESSAGES;
    promptLayers?: PromptLayer[];
  },
): Promise<string> {
  const taskId = (
    await database.query(
      `INSERT INTO agent_task_queue (
         agent_id, issue_id, runtime_id, status, priority, initiator_user_id,
         failure_reason, trigger_summary, started_at, completed_at
       ) VALUES ($1, $2, $3, $4, 0, $5, $6, $7,
                 now() - interval '9 minutes', now() - interval '2 minutes')
       RETURNING id`,
      [
        args.agentId,
        args.issueId,
        args.runtimeId,
        args.status,
        args.userId,
        args.failureReason,
        args.triggerSummary,
      ],
    )
  ).rows[0].id as string;

  for (const [index, message] of args.messages.entries()) {
    await database.query(
      `INSERT INTO task_message (task_id, seq, type, tool, content, input, output, created_at)
       VALUES ($1, $2::int, $3, $4, $5, $6, $7,
               now() - make_interval(mins => 10 - $2::int))`,
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

  // The byte-exact prompt the run actually read, behind the Initial prompt
  // disclosure. This lives in its own table, written by the daemon after it
  // hands the prompt to the runtime — seeding the task alone would silently
  // exercise the no-snapshot fallback instead of the real path.
  if (args.promptLayers) {
    const layers = args.promptLayers;
    const totalBytes = layers.reduce((sum, l) => sum + l.byte_size, 0);
    await database.query(
      `INSERT INTO cerebro_run_prompt_snapshot (
         workspace_id, task_id, agent_id, issue_id, provider, model,
         system_prompt_mode, layers, sha256_original, sha256_redacted,
         total_bytes, redacted
       )
       SELECT workspace_id, $1, $2, $3, 'claude-code', 'claude-opus-5',
              'workdir_file', $4::jsonb, $5, $5, $6, true
         FROM agent WHERE id = $2`,
      [
        taskId,
        args.agentId,
        args.issueId,
        JSON.stringify(layers),
        // Any stable hex stands in for the daemon's real digest; the dialog
        // renders the layers, never recomputes the hash.
        "3f2b91c4e7a05d68b1f4c2093a7de5108c6b4f2719ad30e5b8c71f6a24d09e3b",
        totalBytes,
      ],
    );
  }

  // The immutable permission envelope behind the Task access disclosure.
  await database.query(
    `INSERT INTO cerebro_task_mandate (
       task_id, workspace_id, agent_id, allowed_tools, issued_at, expires_at
     )
     SELECT $1, workspace_id, $2, '["tools:bash", "create_issue"]'::jsonb,
            now() - interval '9 minutes', now() - interval '2 minutes'
       FROM agent WHERE id = $2`,
    [taskId, args.agentId],
  );

  return taskId;
}

/** Open the Logs dialog from the issue's execution log. */
async function openTranscript(page: Page, slug: string, issueId: string) {
  await page.goto(`/${slug}/issues/${issueId}`, { waitUntil: "domcontentloaded" });
  // The execution log lives behind the Runs tab of the issue's Activity panel.
  const runsTab = page.getByRole("tab", { name: "Runs" });
  await expect(runsTab).toBeVisible({ timeout: 30_000 });
  await runsTab.click();
  // The issue surface labels it "Expand transcript"; the agent surface
  // labels it "View transcript". Same button, same dialog.
  const transcriptButton = page
    .getByRole("button", { name: /^(View|Expand) transcript$/ })
    .first();
  await expect(transcriptButton).toBeVisible({ timeout: 30_000 });
  await transcriptButton.click();
  const dialog = page.getByRole("dialog").first();
  await expect(dialog).toBeVisible({ timeout: 15_000 });
  return dialog;
}

async function setExpandMode(page: Page, name: "Focus" | "Expand all" | "Collapse all") {
  await page.getByRole("button", { name: "Expand mode" }).click();
  await page.getByRole("menuitemradio", { name: new RegExp(`^${name}`) }).click();
  // The radio menu stays open after a choice; close it so it does not sit
  // over the transcript in the screenshot.
  if ((await page.getByRole("menuitemradio").count()) > 0) {
    await page.keyboard.press("Escape");
  }
  await expect(page.getByRole("menuitemradio")).toHaveCount(0);
}

test("FIR-3782 — Logs dialog: Focus, Expand all, Collapse all, failure card, disclosures", async ({
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

  // The run-prompt disclosure ships behind the sessions flag; turn it on so
  // this run proves it survived the port.
  await api.setWorkspaceFeatureFlag("cerebro_comment_chapters", true);

  const database = new pg.Client(DATABASE_URL);
  await database.connect();

  const suffix = Date.now().toString(36);
  const userId = (
    await database.query(`SELECT id FROM "user" WHERE email = $1 LIMIT 1`, [
      "e2e@multica.ai",
    ])
  ).rows[0].id as string;
  const runtimeId = (
    await database.query(
      `INSERT INTO agent_runtime (
         workspace_id, name, runtime_mode, provider, status, device_info,
         metadata, last_seen_at
       ) VALUES ($1, $2, 'cloud', 'firtal-gateway', 'online', $2, '{}'::jsonb, now())
       RETURNING id`,
      [workspace.id, `FIR-3782 runtime ${suffix}`],
    )
  ).rows[0].id as string;
  const agentId = (
    await database.query(
      `INSERT INTO agent (
         workspace_id, name, runtime_mode, runtime_config, runtime_id,
         visibility, max_concurrent_tasks, owner_id
       ) VALUES ($1, $2, 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
       RETURNING id`,
      [workspace.id, `FIR-3782 Agent ${suffix}`, runtimeId, userId],
    )
  ).rows[0].id as string;

  const okIssue = await api.createIssue(`FIR-3782 completed run ${suffix}`);
  const failIssue = await api.createIssue(`FIR-3782 failed run ${suffix}`);

  try {
    await seedRun(database, {
      agentId,
      issueId: okIssue.id,
      runtimeId,
      userId,
      status: "completed",
      failureReason: null,
      triggerSummary:
        "Fix the login redirect so it keeps the workspace slug. Reproduce first, then fix, then run the auth tests.",
      messages: COMPLETED_MESSAGES,
      promptLayers: PROMPT_LAYERS,
    });
    await seedRun(database, {
      agentId,
      issueId: failIssue.id,
      runtimeId,
      userId,
      status: "failed",
      failureReason: "agent_error.process_failure",
      triggerSummary:
        "Run the pending migrations against staging and report what changed.",
      messages: FAILED_MESSAGES,
    });

    // ── 1. Default view: Focus ────────────────────────────────────────────
    const dialog = await openTranscript(page, slug, okIssue.id);
    await expect(page.getByRole("button", { name: "Expand mode" })).toContainText(
      "Focus",
    );
    // Focus keeps agent replies open and folds the tool steps.
    await expect(
      dialog.getByText(/Found it\. The slug is read on line 44/),
    ).toBeVisible();
    await dialog.screenshot({ path: `${SHOT_DIR}/1-focus-default.png` });

    // ── 2. Expand all ─────────────────────────────────────────────────────
    await setExpandMode(page, "Expand all");
    await expect(page.getByRole("button", { name: "Expand mode" })).toContainText(
      "Expand all",
    );
    await expect(dialog.getByText(/Tests  31 passed/)).toBeVisible();
    await dialog.screenshot({ path: `${SHOT_DIR}/2-expand-all.png` });

    // ── 3. Collapse all ───────────────────────────────────────────────────
    await setExpandMode(page, "Collapse all");
    await expect(page.getByRole("button", { name: "Expand mode" })).toContainText(
      "Collapse all",
    );
    await expect(dialog.getByText(/Tests  31 passed/)).toHaveCount(0);
    await dialog.screenshot({ path: `${SHOT_DIR}/3-collapse-all.png` });

    // ── 4. The two fork-only disclosures still open ───────────────────────
    await setExpandMode(page, "Focus");
    await dialog.getByText("Initial prompt", { exact: true }).click();

    // The disclosure must show what the MODEL read, not the comment: the
    // runtime brief is the first layer and opens by default.
    await expect(
      dialog.getByText(/You are a coding agent in the Multica platform/),
    ).toBeVisible();
    await expect(dialog.getByText(/GITHUB_TOKEN=\[REDACTED\]/)).toBeVisible();
    await expect(
      dialog.getByRole("button", { name: /runtime_brief/ }),
    ).toHaveAttribute("aria-pressed", "true");

    // The triggering comment is one part of one later layer — reachable, but
    // no longer the whole story.
    await dialog.getByRole("button", { name: /task_prompt/ }).click();
    await expect(
      dialog.getByText(/keeps the workspace slug/).first(),
    ).toBeVisible();
    await dialog.getByRole("button", { name: /runtime_brief/ }).click();

    await dialog.getByText(/^Task access · \d+ allowed$/).click();
    await expect(dialog.getByText("tools:bash", { exact: true })).toBeVisible();
    await dialog.screenshot({ path: `${SHOT_DIR}/4-run-prompt-and-task-access.png` });
    await page.keyboard.press("Escape");

    // ── 5. Failed run: the failure card sits at the top ───────────────────
    const failDialog = await openTranscript(page, slug, failIssue.id);
    await expect(
      failDialog.getByText("The agent process crashed", { exact: true }),
    ).toBeVisible();
    await expect(failDialog.getByText(/SQLSTATE 23502/).first()).toBeVisible();
    await failDialog.screenshot({ path: `${SHOT_DIR}/5-failed-run-failure-card.png` });

    // ── 6. A run with no recorded prompt still reads cleanly ──────────────
    // This run was seeded WITHOUT a prompt snapshot, which is the ordinary
    // case for older runs and runtimes that never report one. The panel must
    // say so and fall back to the triggering comment, never blank out.
    await failDialog.getByText("Initial prompt", { exact: true }).click();
    await expect(
      failDialog.getByText(/No full prompt was recorded for this run/),
    ).toBeVisible();
    await expect(
      failDialog.getByText(/Run the pending migrations against staging/).first(),
    ).toBeVisible();
    await failDialog.screenshot({ path: `${SHOT_DIR}/6-run-prompt-fallback.png` });
  } finally {
    await api.setWorkspaceFeatureFlag("cerebro_comment_chapters", false);
    await database.query(
      `DELETE FROM agent_task_queue WHERE agent_id = $1`,
      [agentId],
    );
    await database.query(`DELETE FROM agent WHERE id = $1`, [agentId]);
    await database.query(`DELETE FROM agent_runtime WHERE id = $1`, [runtimeId]);
    await database.end();
    await api.cleanup();
  }
});
