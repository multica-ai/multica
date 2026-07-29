// FIR-3212 — E2E coverage for the five agent configuration screens:
// Setup, Swap, Approval, Production prompt and Quality.
//
// Each test drives a real control, reloads, and asserts the outcome survived.
// Where a control changes what a RUN reads, the test asserts the stored
// evidence a run pins at claim time (agent.context_version /
// agent_context_version / cerebro_run_prompt_snapshot) — never a UI-only echo.
//
// These screens live on the LEGACY agent detail pane. The redesign flag
// (`cerebro_agent_page_redesign`) renders a different tab list that does not
// contain them, so it is deliberately left off here.
import "./env";
import { expect, test, type Page } from "@playwright/test";
import pg from "pg";
import { loginAsDefault } from "./helpers";
import { TestApiClient } from "./fixtures";

const DATABASE_URL =
  process.env.DATABASE_URL ??
  "postgres://multica:multica@localhost:5432/multica?sslmode=disable";

// Flags gating the five screens. All default OFF in the registry.
const FLAG_SETUP_CAPABILITIES = "cerebro_agent_setup_capabilities"; // Swap + Approval consequences
const FLAG_PRODUCTION_PROMPT = "cerebro_agent_production_prompt";
const FLAG_QUALITY = "cerebro_agent_quality";
const FLAG_AGENT_PAGE_REDESIGN = "cerebro_agent_page_redesign";

interface Seed {
  api: TestApiClient;
  database: pg.Client;
  slug: string;
  workspaceId: string;
  userId: string;
  agentId: string;
  runtimeId: string;
  otherRuntimeId: string;
}

async function seed(page: Page, flags: string[]): Promise<Seed> {
  const slug = await loginAsDefault(page);
  const api = new TestApiClient();
  await api.login("e2e@multica.ai", "E2E User");
  const workspace = (await api.getWorkspaces())[0]!;
  api.setWorkspaceId(workspace.id);
  api.setWorkspaceSlug(workspace.slug);
  for (const flag of flags) {
    await api.setWorkspaceFeatureFlag(flag, true);
  }

  const database = new pg.Client(DATABASE_URL);
  await database.connect();
  const userId = (
    await database.query(`SELECT id FROM "user" WHERE email = $1 LIMIT 1`, [
      "e2e@multica.ai",
    ])
  ).rows[0].id as string;

  const stamp = Date.now();
  // Both providers must have an entry in the capability matrix
  // (server/internal/cerebro/capabilities/discovery.go), otherwise a swap
  // resolves to StatusUnknown and enumerates nothing — by design.
  const runtimeId = await insertRuntime(
    database,
    workspace.id,
    `FIR-3212 runtime ${stamp}`,
    "claude",
  );
  // A second runtime so the Swap panel has a candidate to compare against.
  const otherRuntimeId = await insertRuntime(
    database,
    workspace.id,
    `FIR-3212 other runtime ${stamp}`,
    "firtal-gateway",
  );

  const agentId = (
    await database.query(
      `INSERT INTO agent (
         workspace_id, name, description, instructions, runtime_mode,
         runtime_config, runtime_id, visibility, max_concurrent_tasks,
         owner_id, context_owner_id, context_version, model
       ) VALUES ($1, $2, $3, $4, 'cloud', '{}'::jsonb, $5, 'workspace', 1,
                 $6, $6, '1.0.0', 'claude-opus-4-8')
       RETURNING id`,
      [
        workspace.id,
        `FIR-3212 Agent ${stamp}`,
        "Original description",
        "Original instructions",
        runtimeId,
        userId,
      ],
    )
  ).rows[0].id as string;

  // Every real agent carries a baseline version row (migration 9100 backfills
  // one). Without it a change request has no base_version to diff against, so
  // the queue never resolves a field diff and the approval consequences panel
  // has nothing to explain.
  await database.query(
    `INSERT INTO agent_context_version (agent_id, version, snapshot, description, created_by)
     SELECT a.id, a.context_version,
            jsonb_build_object(
              'instructions',    a.instructions,
              'description',     a.description,
              'runtime_id',      a.runtime_id,
              'model',           a.model,
              'thinking_level',  a.thinking_level,
              'mcp_config',      a.mcp_config,
              'custom_args',     a.custom_args,
              'runtime_config',  a.runtime_config,
              'skill_ids',       '[]'::jsonb,
              'custom_env_keys', '[]'::jsonb
            ),
            'Initial snapshot (e2e seed)', a.owner_id
       FROM agent a WHERE a.id = $1`,
    [agentId],
  );

  return {
    api,
    database,
    slug,
    workspaceId: workspace.id,
    userId,
    agentId,
    runtimeId,
    otherRuntimeId,
  };
}

async function insertRuntime(
  database: pg.Client,
  workspaceId: string,
  name: string,
  provider: string,
): Promise<string> {
  return (
    await database.query(
      `INSERT INTO agent_runtime (
         workspace_id, daemon_id, name, runtime_mode, provider, status,
         device_info, metadata, last_seen_at
       ) VALUES ($1, NULL, $2, 'cloud', $3, 'online', $2, '{}'::jsonb, now())
       RETURNING id`,
      [workspaceId, name, provider],
    )
  ).rows[0].id as string;
}

async function teardown(s: Seed, flags: string[]) {
  for (const flag of flags) {
    await s.api.setWorkspaceFeatureFlag(flag, false).catch(() => {});
  }
  // Snapshots are immutable (trigger blocks UPDATE) but DELETE is allowed.
  await s.database.query(
    `DELETE FROM cerebro_run_prompt_snapshot WHERE agent_id = $1`,
    [s.agentId],
  );
  await s.database.query(`DELETE FROM agent WHERE id = $1`, [s.agentId]);
  await s.database.query(`DELETE FROM agent_runtime WHERE id = ANY($1::uuid[])`, [
    [s.runtimeId, s.otherRuntimeId],
  ]);
  await s.database.end();
  await s.api.cleanup();
}

async function openAgent(page: Page, s: Seed) {
  await page.goto(`/${s.slug}/agents/${s.agentId}`, {
    waitUntil: "domcontentloaded",
  });
}

/** Insert one run's prompt snapshot — the evidence a run records at claim time. */
async function insertSnapshot(
  s: Seed,
  opts: {
    taskId: string;
    version: string;
    provider: string;
    layers: unknown[];
    totalBytes: number;
    createdAt?: string;
  },
) {
  await s.database.query(
    `INSERT INTO cerebro_run_prompt_snapshot (
       workspace_id, task_id, agent_id, agent_context_version,
       agent_context_version_id, provider,
       model, runtime_version, system_prompt_mode, layers,
       sha256_original, sha256_redacted, total_bytes, redacted, created_at
     ) VALUES ($1, $2, $3, $4,
               (SELECT id FROM agent_context_version WHERE agent_id = $3 AND version = $4),
               $5, 'claude-opus-4-8', '1.2.3', 'native',
               $6::jsonb, $7, $8, $9, false, COALESCE($10::timestamptz, now()))`,
    [
      s.workspaceId,
      opts.taskId,
      s.agentId,
      opts.version,
      opts.provider,
      JSON.stringify(opts.layers),
      `orig${opts.taskId.slice(0, 8)}`.padEnd(64, "0"),
      `redact${opts.taskId.slice(0, 8)}`.padEnd(64, "0"),
      opts.totalBytes,
      opts.createdAt ?? null,
    ],
  );
}

interface QualityMeasurementSeed {
  measurementType: "judge_gate" | "evaluator" | "satisfaction";
  category: string;
  verdict: string;
  score?: number;
  confidence?: number;
  evaluatorVersion: string;
}

async function insertQualityRun(
  s: Seed,
  opts: {
    taskId: string;
    version: string;
    createdAt: string;
    measurements: QualityMeasurementSeed[];
  },
) {
  await s.database.query(
    `INSERT INTO agent_task_queue (
       id, agent_id, runtime_id, status, priority, started_at, completed_at
     ) VALUES ($1, $2, $3, 'completed', 0,
               $4::timestamptz - interval '1 minute', $4::timestamptz)`,
    [opts.taskId, s.agentId, s.runtimeId, opts.createdAt],
  );
  await insertSnapshot(s, {
    taskId: opts.taskId,
    version: opts.version,
    provider: "claude_code",
    totalBytes: 10,
    layers: [],
    createdAt: opts.createdAt,
  });
  const analyticsRunId = (
    await s.database.query(
      `INSERT INTO cerebro_analytics_run (
         workspace_id, run_id, population, source_type,
         agent_id, status, started_at, completed_at
       ) VALUES ($1, $2, 'agent', 'manual', $3, 'completed',
                 $4::timestamptz - interval '1 minute', $4::timestamptz)
       RETURNING id`,
      [s.workspaceId, opts.taskId, s.agentId, opts.createdAt],
    )
  ).rows[0].id as string;

  for (const measurement of opts.measurements) {
    await s.database.query(
      `INSERT INTO cerebro_analytics_quality_measurement (
         analytics_run_id, workspace_id, measurement_type, category,
         verdict, score, confidence, evaluator_version
       ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
      [
        analyticsRunId,
        s.workspaceId,
        measurement.measurementType,
        measurement.category,
        measurement.verdict,
        measurement.score ?? null,
        measurement.confidence ?? null,
        measurement.evaluatorVersion,
      ],
    );
  }
}

// ---------------------------------------------------------------------------
// 1. Setup — edit the versioned instructions and approve them.
// ---------------------------------------------------------------------------
test("Setup: control-first instructions and runtime settings persist in the version a run pins", async ({
  page,
}) => {
  const flags = [FLAG_SETUP_CAPABILITIES];
  const s = await seed(page, flags);
  try {
    await openAgent(page, s);
    await page.getByRole("button", { name: "Instructions", exact: true }).click();

    const instructions = page
      .getByRole("region", { name: "Instructions" })
      .locator('[contenteditable="true"]');
    await expect(instructions).toHaveText("Original instructions");
    await expect(page.getByRole("button", { name: "Reads it" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    await expect(page.getByRole("button", { name: "Every tool" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    await expect(page.getByLabel("Engine", { exact: true })).toHaveValue(
      s.runtimeId,
    );
    await expect(page.getByLabel("Stop after")).toHaveValue("");

    // The title/rationale/buttons block only renders once the form is dirty.
    await instructions.fill("Tightened instructions for FIR-3212");
    await page.getByRole("button", { name: "Skips it" }).click();
    await page.getByRole("button", { name: "One line per connection" }).click();
    await page.getByRole("button", { name: "Drop them" }).click();
    await page.getByLabel("Stop after").fill("18");
    await page.getByLabel("Change title").fill("Tighten the instructions");
    await page.getByRole("button", { name: "Save & approve" }).click();
    await expect(page.getByText(/Change approved/)).toBeVisible();

    // Survives a reload — not just optimistic UI state.
    await page.reload({ waitUntil: "domcontentloaded" });
    await page.getByRole("button", { name: "Instructions" }).click();
    await expect(
      page
        .getByRole("region", { name: "Instructions" })
        .locator('[contenteditable="true"]'),
    ).toHaveText("Tightened instructions for FIR-3212");
    await expect(page.getByRole("button", { name: "Skips it" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    await expect(
      page.getByRole("button", { name: "One line per connection" }),
    ).toHaveAttribute("aria-pressed", "true");
    await expect(page.getByLabel("Stop after")).toHaveValue("18");
    await expect(page.getByRole("button", { name: "Drop them" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );

    // What a run reads: the approved text is on the agent, the version is
    // bumped, and an immutable version row exists for a run to pin.
    const agentRow = (
      await s.database.query(
        `SELECT instructions, context_version, runtime_id, runtime_config FROM agent WHERE id = $1`,
        [s.agentId],
      )
    ).rows[0];
    expect(agentRow.instructions).toBe("Tightened instructions for FIR-3212");
    expect(agentRow.runtime_id).toBe(s.runtimeId);
    expect(agentRow.context_version).not.toBe("1.0.0");
    expect(agentRow.runtime_config.workspace_brief_mode).toBe("off");
    expect(agentRow.runtime_config.tools_brief_mode).toBe("summary");
    expect(agentRow.runtime_config.system_prompt_mode).toBe("replace");
    expect(agentRow.runtime_config.max_turns).toBe(18);

    const versionRow = (
      await s.database.query(
        `SELECT version, snapshot FROM agent_context_version
          WHERE agent_id = $1 ORDER BY created_at DESC LIMIT 1`,
        [s.agentId],
      )
    ).rows[0];
    expect(versionRow.version).toBe(agentRow.context_version);
    expect(versionRow.snapshot.instructions).toBe(
      "Tightened instructions for FIR-3212",
    );
    expect(versionRow.snapshot.runtime_id).toBe(s.runtimeId);
    expect(versionRow.snapshot.runtime_config.workspace_brief_mode).toBe("off");
    expect(versionRow.snapshot.runtime_config.tools_brief_mode).toBe("summary");
    expect(versionRow.snapshot.runtime_config.system_prompt_mode).toBe("replace");
    expect(versionRow.snapshot.runtime_config.max_turns).toBe(18);
  } finally {
    await teardown(s, flags);
  }
});

test("Rich editors: Instructions work on both agent pages and skill files preserve their format", async ({
  page,
}) => {
  const flags: string[] = [];
  const s = await seed(page, flags);
  const frontmatter = "---\nname: fir-4000\ntags:\n  - editor\n---\n";
  const originalSkillBody = "# Original skill body\n";
  const originalMdx = '<Example label="original" />\n';
  let skillId: string | null = null;

  try {
    await openAgent(page, s);
    await page.getByRole("button", { name: "Instructions" }).click();
    const legacyInstructions = page
      .getByRole("region", { name: "Instructions" })
      .locator('[contenteditable="true"]');
    await legacyInstructions.fill("Legacy rich instructions for FIR-4000");
    await page.getByLabel("Change title").fill("Update legacy instructions");
    await page.getByRole("button", { name: "Save & approve" }).click();
    await expect(page.getByText(/Change approved/)).toBeVisible();

    await s.api.setWorkspaceFeatureFlag(FLAG_AGENT_PAGE_REDESIGN, true);
    flags.push(FLAG_AGENT_PAGE_REDESIGN);
    await page.reload({ waitUntil: "domcontentloaded" });
    await page.getByRole("button", { name: "Instructions" }).click();
    const redesignedInstructions = page
      .getByRole("region", { name: "Instructions" })
      .locator('[contenteditable="true"]');
    await expect(redesignedInstructions).toHaveText(
      "Legacy rich instructions for FIR-4000",
    );
    await redesignedInstructions.fill(
      "Redesigned rich instructions for FIR-4000",
    );
    await page.getByLabel("Change title").fill("Update redesigned instructions");
    await page.getByRole("button", { name: "Save & approve" }).click();
    await expect(page.getByText(/Change approved/)).toBeVisible();
    await page.reload({ waitUntil: "domcontentloaded" });
    await page.getByRole("button", { name: "Instructions" }).click();
    await expect(
      page
        .getByRole("region", { name: "Instructions" })
        .locator('[contenteditable="true"]'),
    ).toHaveText("Redesigned rich instructions for FIR-4000");

    skillId = (
      await s.database.query(
        `INSERT INTO skill (workspace_id, name, description, content, created_by, owner_id)
         VALUES ($1, $2, 'FIR-4000 editor fixture', $3, $4, $4)
         RETURNING id`,
        [
          s.workspaceId,
          `FIR-4000 skill ${Date.now()}`,
          `${frontmatter}${originalSkillBody}`,
          s.userId,
        ],
      )
    ).rows[0].id as string;
    await s.database.query(
      `INSERT INTO skill_file (skill_id, path, content) VALUES ($1, 'reference.mdx', $2)`,
      [skillId, originalMdx],
    );

    await page.goto(`/${s.slug}/skills/${skillId}`, {
      waitUntil: "domcontentloaded",
    });
    const editFile = () =>
      page
        .locator("div.flex.h-10.items-center.justify-between.gap-3.border-b.px-4")
        .getByRole("button");

    await editFile().click();
    const skillEditor = page.locator('[contenteditable="true"]');
    await expect(skillEditor).toHaveText("Original skill body");
    await skillEditor.fill("# Edited skill body");
    await page.getByRole("button", { name: "Save changes", exact: true }).click();
    await page.reload({ waitUntil: "domcontentloaded" });
    await editFile().click();
    await expect(page.locator('[contenteditable="true"]')).toHaveText(
      "# Edited skill body",
    );

    await page.getByText("reference.mdx", { exact: true }).click();
    await editFile().click();
    const mdxEditor = page.getByRole("textbox", { name: "File content..." });
    await expect(mdxEditor).toHaveValue(originalMdx);
    await mdxEditor.fill('<Example label="edited" />\n');
    await page.getByRole("button", { name: "Save changes", exact: true }).click();
    await page.reload({ waitUntil: "domcontentloaded" });
    await page.getByText("reference.mdx", { exact: true }).click();
    await editFile().click();
    await expect(
      page.getByRole("textbox", { name: "File content..." }),
    ).toHaveValue('<Example label="edited" />\n');

    const storedSkill = (
      await s.database.query(`SELECT content FROM skill WHERE id = $1`, [skillId])
    ).rows[0].content as string;
    expect(storedSkill.slice(0, frontmatter.length)).toBe(frontmatter);
    expect(storedSkill).toContain("Edited skill body");
  } finally {
    if (skillId) {
      await s.database.query(`DELETE FROM skill WHERE id = $1`, [skillId]);
    }
    await teardown(s, flags);
  }
});

// ---------------------------------------------------------------------------
// 2. Swap — compare the agent against another engine.
// ---------------------------------------------------------------------------
test("Swap: selecting another engine reports what that engine changes", async ({
  page,
}) => {
  const flags = [FLAG_SETUP_CAPABILITIES];
  const s = await seed(page, flags);
  try {
    await openAgent(page, s);
    await page.getByRole("button", { name: "Instructions" }).click();

    await page
      .getByText("Advanced: engine compatibility and swap checks")
      .click();
    await expect(page.getByText("Engine swap")).toBeVisible();
    const select = page.getByLabel("Compare with another engine");

    // The candidate list must contain the other runtime — this fails when the
    // pane forgets to pass `runtimes` down to the context tab.
    await expect(
      select.locator("option", { hasText: "FIR-3212 other runtime" }),
    ).toHaveCount(1);

    await select.selectOption(s.otherRuntimeId);

    // The comparison resolves to a real verdict, not a spinner or an error.
    await expect(page.getByText("Checking that engine…")).toHaveCount(0, {
      timeout: 15000,
    });
    await expect(
      page.getByText("The comparison is unavailable right now."),
    ).toHaveCount(0);
    await expect(
      page
        .getByText("Stops working on")
        .or(page.getByText("Starts working on"))
        .or(
          page.getByText(
            "Nothing changes on this engine — every setting is handled the same way.",
          ),
        )
        .first(),
    ).toBeVisible();

    // Swap is a read-only comparison: it must not mutate the agent's engine.
    const runtimeAfter = (
      await s.database.query(`SELECT runtime_id FROM agent WHERE id = $1`, [
        s.agentId,
      ])
    ).rows[0].runtime_id;
    expect(runtimeAfter).toBe(s.runtimeId);
  } finally {
    await teardown(s, flags);
  }
});

// ---------------------------------------------------------------------------
// 3. Approval — see what approving a pending proposal actually changes.
// ---------------------------------------------------------------------------
test("Approval: a pending proposal shows its consequences and approving applies it", async ({
  page,
}) => {
  const flags = [FLAG_SETUP_CAPABILITIES];
  const s = await seed(page, flags);
  try {
    await openAgent(page, s);
    await page.getByRole("button", { name: "Instructions" }).click();

    // Propose (not Save & approve) so the change request stays pending.
    await page
      .getByRole("region", { name: "Instructions" })
      .locator('[contenteditable="true"]')
      .fill("Proposed instructions for FIR-3212");
    await page
      .getByLabel("Engine", { exact: true })
      .selectOption(s.otherRuntimeId);
    await page.getByLabel("Change title").fill("Proposed change");
    await page.getByRole("button", { name: "Propose", exact: true }).click();
    await expect(page.getByText(/Change proposed/)).toBeVisible();

    // The consequences panel explains the pending proposal before approval.
    await expect(page.getByText("What changes after approval")).toBeVisible();
    await expect(
      page.locator('[data-testid^="approval-"]').first(),
    ).toBeVisible();

    const pending = (
      await s.database.query(
        `SELECT id, status FROM agent_change_request
          WHERE agent_id = $1 ORDER BY created_at DESC LIMIT 1`,
        [s.agentId],
      )
    ).rows[0];
    expect(pending.status).toBe("pending");
    // Still pending means the agent is untouched.
    expect(
      (
        await s.database.query(
          `SELECT instructions, runtime_id FROM agent WHERE id = $1`,
          [s.agentId],
        )
      ).rows[0],
    ).toMatchObject({
      instructions: "Original instructions",
      runtime_id: s.runtimeId,
    });

    // Approving is a two-step confirm: the queue button opens a dialog that
    // carries the real Approve action (plus an optional comment).
    await page.getByRole("button", { name: "Approve", exact: true }).click();
    const confirm = page.getByRole("dialog");
    await expect(confirm.getByText(/Approve change:/)).toBeVisible();
    await confirm.getByRole("button", { name: "Approve", exact: true }).click();
    await expect(confirm).toHaveCount(0);

    await page.reload({ waitUntil: "domcontentloaded" });
    await page.getByRole("button", { name: "Instructions" }).click();
    await expect(
      page
        .getByRole("region", { name: "Instructions" })
        .locator('[contenteditable="true"]'),
    ).toHaveText("Proposed instructions for FIR-3212");

    const reviewed = (
      await s.database.query(
        `SELECT status, reviewed_by FROM agent_change_request WHERE id = $1`,
        [pending.id],
      )
    ).rows[0];
    // Approving applies the change in one step, so the request lands on the
    // terminal "merged" state rather than resting on "approved".
    expect(reviewed.status).toBe("merged");
    expect(reviewed.reviewed_by).toBe(s.userId);
    expect(
      (
        await s.database.query(
          `SELECT instructions, runtime_id FROM agent WHERE id = $1`,
          [s.agentId],
        )
      ).rows[0],
    ).toMatchObject({
      instructions: "Proposed instructions for FIR-3212",
      runtime_id: s.otherRuntimeId,
    });
    const approvedSnapshot = (
      await s.database.query(
        `SELECT snapshot FROM agent_context_version
          WHERE agent_id = $1 ORDER BY created_at DESC LIMIT 1`,
        [s.agentId],
      )
    ).rows[0].snapshot;
    expect(approvedSnapshot.runtime_id).toBe(s.otherRuntimeId);
  } finally {
    await teardown(s, flags);
  }
});

// ---------------------------------------------------------------------------
// 4. Production prompt — the recorded evidence of what a run read.
// ---------------------------------------------------------------------------
test("Production prompt: recorded parts and pending runtime differences stay visible", async ({
  page,
}) => {
  const flags = [FLAG_SETUP_CAPABILITIES, FLAG_PRODUCTION_PROMPT];
  const s = await seed(page, flags);
  try {
    await openAgent(page, s);
    await page.getByRole("button", { name: "Production prompt" }).click();

    // Empty state before any run has recorded a prompt.
    await expect(
      page.getByText(/No recorded run exists yet\./),
    ).toBeVisible();

    const olderTaskId = "3212a001-0000-4000-8000-000000000001";
    const newerTaskId = "3212a002-0000-4000-8000-000000000002";
    await insertSnapshot(s, {
      taskId: olderTaskId,
      version: "1.0.0",
      provider: "claude_code",
      totalBytes: 120,
      createdAt: "2026-07-16T08:00:00Z",
      layers: [
        {
          name: "runtime_brief",
          delivery: "system_prompt",
          byte_size: 120,
          sha256_original: "a".repeat(64),
          sha256_redacted: "a".repeat(64),
          content_redacted: "## Agent Identity\n\nOlder run content",
        },
        {
          name: "task_prompt",
          delivery: "user_prompt",
          byte_size: 40,
          sha256_original: "d".repeat(64),
          sha256_redacted: "d".repeat(64),
          content_redacted: "Older case content",
        },
      ],
    });
    await insertSnapshot(s, {
      taskId: newerTaskId,
      version: "2.0.0",
      provider: "firtal-gateway",
      totalBytes: 2048,
      createdAt: "2026-07-17T08:00:00Z",
      layers: [
        {
          name: "runtime_brief",
          delivery: "system_prompt",
          byte_size: 1024,
          sha256_original: "b".repeat(64),
          sha256_redacted: "b".repeat(64),
          content_redacted:
            "## Agent Identity\n\nOriginal instructions\n\n## Available Commands\n\nShared brief layer content\n\n### Connections & MCP tools\n\nTools layer content\n\n## Agent Skills\n\nSkill layer content",
        },
        {
          name: "task_prompt",
          delivery: "user_prompt",
          byte_size: 1024,
          sha256_original: "c".repeat(64),
          sha256_redacted: "c".repeat(64),
          content_redacted: "Case layer content",
        },
      ],
    });

    await page.reload({ waitUntil: "domcontentloaded" });
    await page.getByRole("button", { name: "Production prompt" }).click();

    // The default view exposes both the parts and the exact selected text.
    await expect(
      page.getByRole("region", { name: "Prompt evidence" }),
    ).toBeVisible();
    await expect(page.getByText("The parts it is made of")).toBeVisible();
    await expect(page.getByText("Original instructions")).toBeVisible();
    await expect(
      page.getByRole("link", { name: "Edit instructions" }),
    ).toHaveAttribute("href", "?tab=context#agent-instructions");

    // Technical provenance is secondary, while exact text stays visible.
    await page.getByText("Technical evidence", { exact: false }).click();
    await expect(
      page.getByText("Captured from the run — not reconstructed"),
    ).toBeVisible();

    // Drive the parts nav: recorded role, tools and case stay understandable.
    await page.getByRole("button", { name: /Tools & connections/ }).click();
    await expect(page.getByText("Tools layer content")).toBeVisible();
    await page.getByRole("button", { name: /Case itself/ }).click();
    await expect(page.getByText("Case layer content")).toBeVisible();

    // Drive the run selector: switching runs shows that run's own evidence.
    await page.locator("#cerebro-prompt-snapshot-run").selectOption(olderTaskId);
    await expect(page.getByText("Older run content")).toBeVisible();
    await expect(page.getByText("Case layer content")).toHaveCount(0);

    // Create a real pending proposal through the control-first surface.
    await page.getByRole("button", { name: "Instructions", exact: true }).click();
    await page.getByRole("button", { name: "Drop them" }).click();
    await page.getByLabel("Stop after").fill("18");
    await page.getByLabel("Change title").fill("Bound the next run");
    await page.getByRole("button", { name: "Propose", exact: true }).click();
    await expect(page.getByText(/Change proposed/)).toBeVisible();

    await page.getByRole("button", { name: "Production prompt" }).click();
    await page.getByRole("button", { name: "Difference" }).click();
    await page.getByRole("button", { name: /Run controls/ }).click();
    await expect(page.getByText("+ System prompt: replace")).toBeVisible();
    await expect(page.getByText("+ Stop after: 18")).toBeVisible();

    // Snapshots are immutable run evidence — the DB refuses any rewrite.
    await expect(
      s.database.query(
        `UPDATE cerebro_run_prompt_snapshot SET total_bytes = 1 WHERE task_id = $1`,
        [newerTaskId],
      ),
    ).rejects.toThrow(/immutable/);
  } finally {
    await teardown(s, flags);
  }
});

// ---------------------------------------------------------------------------
// 5. Quality — per-version attribution over the runs that actually ran.
// ---------------------------------------------------------------------------
test("Quality: each config version is measured on the runs that ran under it", async ({
  page,
}) => {
  const flags = [FLAG_QUALITY];
  const s = await seed(page, flags);
  try {
    await openAgent(page, s);
    await page.getByRole("button", { name: "Quality" }).click();

    await expect(
      page.getByRole("heading", { name: "Did the change work?" }),
    ).toBeVisible();
    await expect(
      page.getByText(/No runs have recorded a config version yet\./),
    ).toBeVisible();

    await s.database.query(
      `INSERT INTO agent_context_version (
         agent_id, version, snapshot, description, created_by
       ) VALUES ($1, '2.0.0', '{}'::jsonb, 'FIR-3212 quality e2e', $2)`,
      [s.agentId, s.userId],
    );

    // Two measured runs on 1.0.0, one unmeasured run on 2.0.0 — attribution
    // must not pool them, and every displayed denominator must come from the
    // stored PostgreSQL rows rather than a UI fixture.
    await insertQualityRun(s, {
      taskId: "3212b001-0000-4000-8000-000000000001",
      version: "1.0.0",
      createdAt: "2026-07-17T07:00:00Z",
      measurements: [
        {
          measurementType: "judge_gate",
          category: "solution",
          verdict: "pass",
          score: 0.8,
          confidence: 0.9,
          evaluatorVersion: "judge-v1",
        },
        {
          measurementType: "satisfaction",
          category: "reaction:thumbs-up:member",
          verdict: "thumbs-up",
          evaluatorVersion: "human-v1",
        },
        {
          measurementType: "satisfaction",
          category: "human_approval:release:quality",
          verdict: "approved",
          evaluatorVersion: "human-v1",
        },
      ],
    });
    await insertQualityRun(s, {
      taskId: "3212b002-0000-4000-8000-000000000002",
      version: "1.0.0",
      createdAt: "2026-07-17T07:05:00Z",
      measurements: [
        {
          measurementType: "evaluator",
          category: "solution",
          verdict: "fail",
          score: 0.4,
          evaluatorVersion: "evaluator-v1",
        },
        {
          measurementType: "satisfaction",
          category: "human_approval:release:quality",
          verdict: "rejected",
          evaluatorVersion: "human-v1",
        },
      ],
    });
    await insertQualityRun(s, {
      taskId: "3212b003-0000-4000-8000-000000000003",
      version: "2.0.0",
      createdAt: "2026-07-17T07:10:00Z",
      measurements: [],
    });

    await page.reload({ waitUntil: "domcontentloaded" });
    await page.getByRole("button", { name: "Quality" }).click();

    const rowFor = (version: string) =>
      page.getByRole("row").filter({ hasText: version });
    await expect(rowFor("1.0.0")).toBeVisible();
    await expect(rowFor("2.0.0")).toBeVisible();
    // Runs are attributed per version, not summed across versions.
    await expect(rowFor("1.0.0").getByRole("cell", { name: "2", exact: true })).toBeVisible();
    await expect(rowFor("2.0.0").getByRole("cell", { name: "1", exact: true })).toBeVisible();

    // The measured row proves the real SQL aggregation reaches the real UI:
    // score 0.60 = (0.8 + 0.4) / 2, while confidence 0.90 uses its own single
    // observation denominator. Human signals also retain their run sample.
    await expect(rowFor("1.0.0").getByText("0.60")).toBeVisible();
    await expect(rowFor("1.0.0").getByText(/1\s*\/\s*2 passed · 2 of 2 runs measured/)).toBeVisible();
    await expect(rowFor("1.0.0").getByText("0.90")).toBeVisible();
    await expect(rowFor("1.0.0").getByText("across 1 measurement")).toBeVisible();
    await expect(rowFor("1.0.0").getByText("1 reactions")).toBeVisible();
    await expect(rowFor("1.0.0").getByText("1 approval")).toBeVisible();
    await expect(rowFor("1.0.0").getByText("1 rejection")).toBeVisible();
    await expect(rowFor("1.0.0").getByText("(2 of 2 runs)")).toBeVisible();

    // The 2.0.0 run has no observations. It remains an honest missing state
    // instead of inheriting the previous version's numbers or showing zeroes.
    await expect(rowFor("2.0.0").getByText("No measurements yet")).toBeVisible();
    await expect(rowFor("2.0.0").getByText("No signals yet")).toBeVisible();
  } finally {
    await teardown(s, flags);
  }
});
