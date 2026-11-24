#!/usr/bin/env node
/**
 * Record real Multica Web App demo clips (English UI, light + dark) for Multica Docs.
 *
 * Usage (inside a `make up C=api,web ARGS="--ephemeral"` environment):
 *   make env-exec ARGS="-- node scripts/docs-demos/record.mjs"
 *   make env-exec ARGS="-- node scripts/docs-demos/record.mjs --only assign,mention"
 *
 * Outputs for each clip <slug>:
 *   apps/docs/public/videos/demos/<slug>-light.mp4
 *   apps/docs/public/videos/demos/<slug>-dark.mp4
 *   apps/docs/public/videos/demos/<slug>-light.jpg   (representative poster frame)
 *   apps/docs/public/videos/demos/<slug>-dark.jpg
 * And updates apps/docs/components/demos/clip-manifest.ts with exact durations & chapter timestamps.
 */
import { createRequire } from "node:module";
import { execFileSync, spawnSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";

const repoRoot = execFileSync("git", ["rev-parse", "--show-toplevel"]).toString().trim();
const { chromium } = createRequire(join(repoRoot, "package.json"))("@playwright/test");

const env = process.env;
if (!env.PORT || !/^dev-/.test(env.MULTICA_DEV_PROFILE ?? "")) {
  console.error('[docs-demos] run through `make env-exec ARGS="-- node scripts/docs-demos/record.mjs"`.');
  process.exit(1);
}

const outDir = join(repoRoot, "apps/docs/public/videos/demos");
const manifestJsonPath = join(outDir, "manifest.json");
const manifestTsPath = join(repoRoot, "apps/docs/components/demos/clip-manifest.ts");
mkdirSync(outDir, { recursive: true });

const argv = process.argv.slice(2);
let onlySet = null;
for (let i = 0; i < argv.length; i++) {
  if (argv[i] === "--only" && argv[i + 1]) {
    onlySet = new Set(argv[++i].split(",").map((s) => s.trim()).filter(Boolean));
  }
}

const base = `http://localhost:${env.FRONTEND_PORT}`;
const apiBase = `http://localhost:${env.PORT}`;
const profilesHome = env.MULTICA_DEV_PROFILES_HOME || join(homedir(), ".multica", "profiles");
const profile = JSON.parse(readFileSync(join(profilesHome, env.MULTICA_DEV_PROFILE, "config.json"), "utf8"));
const slug = JSON.parse(execFileSync("bash", [join(repoRoot, "scripts", "dev-env.sh"), "status", "--json"]).toString()).workspace_slug;

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const log = (m) => console.log(`[docs-demos] ${m}`);

function psql(sql) {
  if (!env.DATABASE_URL) return "";
  const res = spawnSync("psql", [env.DATABASE_URL, "-v", "ON_ERROR_STOP=0", "-tAc", sql], { encoding: "utf8" });
  if (res.status !== 0 && res.stderr) log(`psql warn: ${res.stderr.trim().slice(0, 200)}`);
  return (res.stdout || "").trim();
}

async function api(method, path, body, extraHeaders = {}) {
  const res = await fetch(`${apiBase}${path}`, {
    method,
    headers: {
      Authorization: `Bearer ${profile.token}`,
      "X-Workspace-ID": profile.workspace_id,
      "Content-Type": "application/json",
      ...extraHeaders,
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await res.text();
  if (!res.ok) throw new Error(`${method} ${path} -> ${res.status} ${text.slice(0, 260)}`);
  return text ? JSON.parse(text) : null;
}

async function safeApi(method, path, body, extraHeaders = {}) {
  try {
    return await api(method, path, body, extraHeaders);
  } catch (err) {
    log(`warn: ${err.message}`);
    return null;
  }
}

// ---------------------------------------------------------------------------
// Daemon helper: keep ALL workspace runtimes online & answer queued runs
// ---------------------------------------------------------------------------
const DAEMON_ID = "docs-demo-recorder";
let demoRuntime = null;
let heartbeatTimer = null;

async function pulseAllRuntimes() {
  const raw = (await safeApi("GET", "/api/runtimes")) ?? [];
  const list = Array.isArray(raw) ? raw : raw.runtimes ?? [];
  for (const rt of list) {
    if (rt?.id) {
      await safeApi("POST", "/api/daemon/heartbeat", { runtime_id: rt.id });
    }
  }
  if (demoRuntime?.id) {
    await safeApi("POST", "/api/daemon/heartbeat", { runtime_id: demoRuntime.id });
  }
}

async function ensureDemoRuntime() {
  const reg = await api("POST", "/api/daemon/register", {
    workspace_id: profile.workspace_id,
    daemon_id: DAEMON_ID,
    device_name: "Maya's MacBook Pro",
    cli_version: "v0.12.0",
    runtimes: [{ name: "Claude Code", type: "claude", version: "2.1.0", status: "online" }],
  });
  demoRuntime = reg?.runtimes?.[0];
  if (!demoRuntime?.id) throw new Error("failed to register demo runtime");
  await pulseAllRuntimes();
  heartbeatTimer = setInterval(() => {
    pulseAllRuntimes().catch(() => {});
  }, 12_000);
  return demoRuntime;
}

async function ensureAgent(name, description, instructions) {
  const raw = (await api("GET", "/api/agents")) ?? [];
  const list = Array.isArray(raw) ? raw : raw.agents ?? [];
  const existing = list.find((a) => a.name === name && a.runtime_id === demoRuntime.id);
  if (existing) return existing;
  return api("POST", "/api/agents", {
    name,
    description,
    instructions,
    runtime_id: demoRuntime.id,
    visibility: "workspace",
    max_concurrent_tasks: 20,
  });
}

function clearStaleQueuedTasks() {
  psql("UPDATE agent_task_queue SET status = 'completed', completed_at = NOW() WHERE status IN ('queued', 'dispatched', 'running');");
}

async function claimTaskMatching(predicate, timeoutMs = 6000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const claimed = await safeApi("POST", `/api/daemon/runtimes/${demoRuntime.id}/tasks/claim`, {});
    const task = claimed?.task;
    if (task) {
      if (predicate(task)) return task;
      await safeApi("POST", `/api/daemon/tasks/${task.id}/start`);
      await safeApi("POST", `/api/daemon/tasks/${task.id}/complete`, { output: "Completed." });
      continue;
    }
    await sleep(200);
  }
  return null;
}

async function startTask(taskId) {
  await api("POST", `/api/daemon/tasks/${taskId}/start`);
}

async function reportMessage(taskId, msg) {
  await safeApi("POST", `/api/daemon/tasks/${taskId}/messages`, msg);
}

async function completeTask(taskId, output) {
  await api("POST", `/api/daemon/tasks/${taskId}/complete`, { output });
}

async function reportUsage(taskId, inputTokens, outputTokens, costUsd = 0.24) {
  await safeApi("POST", `/api/daemon/tasks/${taskId}/usage`, {
    usage: [
      {
        provider: "anthropic",
        model: "claude-sonnet-4-6",
        input_tokens: inputTokens,
        output_tokens: outputTokens,
        cache_read_tokens: Math.round(inputTokens * 0.45),
        cache_write_tokens: Math.round(inputTokens * 0.12),
        cost_usd_ticks: Math.round(costUsd * 1e10),
      },
    ],
  });
}

// ---------------------------------------------------------------------------
// Cursor + Recorder helpers
// ---------------------------------------------------------------------------
const VIEWPORT = { width: 1280, height: 800 };
const CAPTURE_SCALE = 2;

const CURSOR_CSS = `
  nextjs-portal { display: none !important; }
  #__demo_cursor {
    position: fixed;
    left: -40px;
    top: -40px;
    width: 22px;
    height: 22px;
    pointer-events: none;
    z-index: 2147483647;
    transform: translate(-2px, -2px);
    transition: left 240ms cubic-bezier(0.22, 1, 0.36, 1), top 240ms cubic-bezier(0.22, 1, 0.36, 1);
    filter: drop-shadow(0 2px 4px rgba(0,0,0,0.28));
  }
  #__demo_ripple {
    position: fixed;
    left: -40px;
    top: -40px;
    width: 28px;
    height: 28px;
    margin-left: -14px;
    margin-top: -14px;
    border-radius: 999px;
    border: 2px solid oklch(0.56 0.19 256);
    pointer-events: none;
    z-index: 2147483646;
    opacity: 0;
    transform: scale(0.4);
  }
  #__demo_ripple.pop {
    animation: __demo_pop 360ms cubic-bezier(0.16, 1, 0.3, 1) forwards;
  }
  @keyframes __demo_pop {
    0% { opacity: 0.9; transform: scale(0.35); }
    100% { opacity: 0; transform: scale(1.45); }
  }
`;

async function injectCursor(page) {
  await page.addStyleTag({ content: CURSOR_CSS }).catch(() => {});
  await page.evaluate(() => {
    if (document.getElementById("__demo_cursor")) return;
    const c = document.createElement("div");
    c.id = "__demo_cursor";
    c.innerHTML = `<svg viewBox="0 0 24 24" width="22" height="22" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M5.5 3.2L18.8 12.6L12.6 13.7L9.5 19.6L5.5 3.2Z" fill="#111113" stroke="#ffffff" stroke-width="1.6" stroke-linejoin="round"/></svg>`;
    const r = document.createElement("div");
    r.id = "__demo_ripple";
    document.body.append(r, c);
  }).catch(() => {});
}

async function moveCursor(page, x, y) {
  await page.evaluate(([cx, cy]) => {
    const c = document.getElementById("__demo_cursor");
    if (c) { c.style.left = `${cx}px`; c.style.top = `${cy}px`; }
  }, [x, y]).catch(() => {});
  await page.mouse.move(x, y, { steps: 6 });
  await sleep(230);
}

async function clickAt(page, x, y, { pauseBefore = 100, pauseAfter = 420 } = {}) {
  await moveCursor(page, x, y);
  await sleep(pauseBefore);
  await page.evaluate(([cx, cy]) => {
    const r = document.getElementById("__demo_ripple");
    if (!r) return;
    r.classList.remove("pop");
    r.style.left = `${cx}px`;
    r.style.top = `${cy}px`;
    void r.offsetWidth;
    r.classList.add("pop");
  }, [x, y]).catch(() => {});
  await page.mouse.click(x, y);
  await sleep(pauseAfter);
}

async function smoothClick(page, locator, opts = {}) {
  const el = typeof locator === "string" ? page.locator(locator).first() : locator.first();
  await el.waitFor({ state: "visible", timeout: 4500 });
  const box = await el.boundingBox();
  if (!box) throw new Error("smoothClick: no boundingBox");
  await clickAt(page, Math.round(box.x + box.width / 2), Math.round(box.y + box.height / 2), opts);
}

async function trySmoothClick(page, locator, opts = {}) {
  try {
    await smoothClick(page, locator, opts);
    return true;
  } catch {
    return false;
  }
}

/** Find a visible button inside a horizontal x range (e.g., right properties panel x > 860). */
async function clickButtonInZone(page, nameRegex, { minX = 0, maxX = 1280, minY = 0, maxY = 800, pauseAfter = 450 } = {}) {
  const loc = page.getByRole("button", { name: nameRegex });
  const count = await loc.count();
  for (let i = 0; i < count; i++) {
    const el = loc.nth(i);
    if (!(await el.isVisible().catch(() => false))) continue;
    const box = await el.boundingBox();
    if (!box) continue;
    const cx = box.x + box.width / 2;
    const cy = box.y + box.height / 2;
    if (cx >= minX && cx <= maxX && cy >= minY && cy <= maxY) {
      await clickAt(page, Math.round(cx), Math.round(cy), { pauseAfter });
      return true;
    }
  }
  return false;
}

/** Click an option/menuitem inside the open popover (avoiding left sidebar matches). */
async function clickPopoverItem(page, textRegex, { pauseAfter = 450 } = {}) {
  const selectors = [
    page.locator('[role="listbox"], [role="menu"], [data-slot="popover-content"], [data-base-ui-portal]').getByText(textRegex),
    page.getByRole("option", { name: textRegex }),
    page.getByRole("menuitem", { name: textRegex }),
  ];
  for (const loc of selectors) {
    const count = await loc.count();
    for (let i = count - 1; i >= 0; i--) {
      const el = loc.nth(i);
      if (!(await el.isVisible().catch(() => false))) continue;
      const box = await el.boundingBox();
      if (!box || box.x < 280) continue;
      await clickAt(page, Math.round(box.x + box.width / 2), Math.round(box.y + box.height / 2), { pauseAfter });
      return true;
    }
  }
  return false;
}

async function focusAndTypeComment(page, text) {
  const shell = page.locator('[data-testid="comment-composer-shell"]').first();
  if (await shell.isVisible({ timeout: 2500 }).catch(() => false)) {
    await smoothClick(page, shell, { pauseAfter: 240 });
  }
  const editor = page
    .locator('[data-mc="comment-composer"] [contenteditable="true"], [data-mc="comment-composer"] textarea')
    .or(page.getByPlaceholder("Leave a comment..."))
    .first();
  await editor.waitFor({ state: "visible", timeout: 4500 });
  await editor.focus().catch(() => {});
  await sleep(120);
  await page.keyboard.type(text, { delay: 16 });
  await sleep(260);
}

async function submitActiveComment(page) {
  const sendBtn = page
    .locator('[data-mc="comment-composer"] button[aria-label*="Send" i], [data-mc="comment-composer"] button[type="submit"]')
    .last();
  if (!(await trySmoothClick(page, sendBtn, { pauseAfter: 450 }))) {
    await page.keyboard.press("Meta+Enter");
    await sleep(450);
  }
}

async function openPage(browser, path, theme) {
  const context = await browser.newContext({
    viewport: VIEWPORT,
    locale: "en-US",
    deviceScaleFactor: CAPTURE_SCALE,
    colorScheme: theme,
  });
  await context.addInitScript(
    ([token, t]) => {
      localStorage.setItem("multica_token", token);
      localStorage.setItem("theme", t);
      localStorage.setItem("multica:chat:isOpen", "false");
    },
    [profile.token, theme],
  );
  const page = await context.newPage();
  await page.goto(`${base}/${slug}${path}`, { waitUntil: "domcontentloaded" });
  await page.waitForLoadState("networkidle").catch(() => {});
  await sleep(600);
  await injectCursor(page);
  return { context, page };
}

async function recordClip(browser, clipSlug, theme, path, run) {
  const { context, page } = await openPage(browser, path, theme);
  const cdp = await context.newCDPSession(page);
  const shots = [];
  const marks = [];
  let posterIdx = null;
  let startedAt = 0;
  let stopped = false;

  const mark = (id) => {
    const at = startedAt ? Math.max(0, (Date.now() - startedAt) / 1000) : 0;
    marks.push({ id, at: Math.round(at * 10) / 10 });
  };
  const setPoster = () => {
    posterIdx = Math.max(0, shots.length - 1);
  };

  const loop = (async () => {
    const interval = 1000 / 12;
    while (!stopped) {
      const t = Date.now();
      if (!startedAt) startedAt = t;
      try {
        const { data } = await cdp.send("Page.captureScreenshot", {
          format: "jpeg",
          quality: 94,
          clip: { x: 0, y: 0, width: VIEWPORT.width, height: VIEWPORT.height, scale: CAPTURE_SCALE },
          optimizeForSpeed: true,
        });
        shots.push({ t, data });
      } catch {
        break;
      }
      const wait = interval - (Date.now() - t);
      if (wait > 0) await sleep(wait);
    }
  })();

  try {
    await sleep(280);
    await run({ page, mark, setPoster });
    await sleep(650);
  } finally {
    stopped = true;
    await loop.catch(() => {});
    await cdp.detach().catch(() => {});
    await context.close().catch(() => {});
  }

  if (shots.length < 2) throw new Error(`${clipSlug}-${theme}: too few frames`);

  const tmpDir = join(outDir, `.tmp-${clipSlug}-${theme}`);
  rmSync(tmpDir, { recursive: true, force: true });
  mkdirSync(tmpDir, { recursive: true });

  const lines = ["ffconcat version 1.0"];
  shots.forEach((s, i) => {
    const fname = `${String(i).padStart(5, "0")}.jpg`;
    writeFileSync(join(tmpDir, fname), Buffer.from(s.data, "base64"));
    const nextT = shots[i + 1]?.t ?? s.t + 650;
    const dur = Math.max(0.04, (nextT - s.t) / 1000);
    lines.push(`file '${fname}'`, `duration ${dur.toFixed(3)}`);
  });
  lines.push(`file '${String(shots.length - 1).padStart(5, "0")}.jpg'`);
  writeFileSync(join(tmpDir, "frames.ffconcat"), `${lines.join("\n")}\n`);

  const mp4 = join(outDir, `${clipSlug}-${theme}.mp4`);
  const jpg = join(outDir, `${clipSlug}-${theme}.jpg`);

  const enc = spawnSync(
    "ffmpeg",
    [
      "-y", "-loglevel", "error",
      "-f", "concat", "-safe", "0", "-i", join(tmpDir, "frames.ffconcat"),
      "-vf", "scale=1280:800:flags=lanczos",
      "-c:v", "libx264",
      "-pix_fmt", "yuv420p",
      "-profile:v", "high",
      "-crf", "24",
      "-preset", "slow",
      "-movflags", "+faststart",
      mp4,
    ],
    { stdio: "inherit" },
  );
  if (enc.status !== 0) throw new Error(`ffmpeg mp4 failed for ${clipSlug}-${theme}`);

  const chosenPoster = posterIdx !== null ? posterIdx : Math.floor(shots.length * 0.65);
  const posterJpg = join(tmpDir, `${String(Math.min(chosenPoster, shots.length - 1)).padStart(5, "0")}.jpg`);
  const pEnc = spawnSync(
    "ffmpeg",
    ["-y", "-loglevel", "error", "-i", posterJpg, "-vf", "scale=1280:800:flags=lanczos", "-q:v", "4", jpg],
    { stdio: "inherit" },
  );
  if (pEnc.status !== 0) throw new Error(`ffmpeg poster failed for ${clipSlug}-${theme}`);

  rmSync(tmpDir, { recursive: true, force: true });

  const duration = Math.round(((shots.at(-1).t - shots[0].t) / 1000 + 0.65) * 10) / 10;
  const sizeKb = Math.round(readFileSync(mp4).length / 1024);
  log(`${clipSlug}-${theme}: ${shots.length} frames, ${duration}s, ${sizeKb} KB`);
  return { duration, marks };
}

// ---------------------------------------------------------------------------
// Seed shared realistic workspace objects
// ---------------------------------------------------------------------------
async function seedShared() {
  clearStaleQueuedTasks();
  await ensureDemoRuntime();

  const me = await api("GET", "/api/me");

  const engineer = await ensureAgent(
    "Engineer",
    "Builds features, runs tests, and opens pull requests.",
    "Read the issue and discussion, implement with tests, and move the issue to In Review when ready.",
  );
  const reviewer = await ensureAgent(
    "Reviewer",
    "Read-only code review for diffs, edge cases, and regressions.",
    "Review PR diffs carefully and return actionable findings with file paths and line numbers.",
  );
  const docsAgent = await ensureAgent(
    "Docs",
    "Keeps API docs and release notes in sync with shipped changes.",
    "Write concise technical documentation matching the editorial style guide.",
  );

  // Skills
  const existingSkills = (await safeApi("GET", "/api/skills")) ?? [];
  const skillsList = Array.isArray(existingSkills) ? existingSkills : existingSkills.skills ?? [];
  const ensureSkill = async (name, description, content) => {
    const found = skillsList.find((s) => s.name === name);
    if (found) return found;
    return safeApi("POST", "/api/skills", { name, description, content });
  };
  const tddSkill = await ensureSkill(
    "tdd",
    "Write a failing test first, make it pass with minimal changes, then refactor.",
    "# Test-Driven Development\n\n1. Add a focused failing test that reproduces the behavior.\n2. Implement the smallest change to make the suite pass.\n3. Refactor while keeping every check green.",
  );
  await ensureSkill(
    "delivery-brief",
    "Format handoff updates with outcome first, PR summary, and open decisions.",
    "# Delivery Brief\n\nOpen with the conclusion in one line, list the PR link and verification results, and highlight any decisions that need human approval.",
  );
  if (tddSkill?.id) {
    await safeApi("POST", `/api/agents/${engineer.id}/skills/add`, { skill_ids: [tddSkill.id] });
  }

  // Squad
  const existingSquads = (await safeApi("GET", "/api/squads")) ?? [];
  const squadsList = Array.isArray(existingSquads) ? existingSquads : existingSquads.squads ?? [];
  let squad = squadsList.find((s) => s.name === "Platform Team");
  if (!squad) {
    squad = await safeApi("POST", "/api/squads", {
      name: "Platform Team",
      description: "Implements, reviews, and documents backend & API changes end to end.",
      leader_id: engineer.id,
    });
    if (squad?.id) {
      await safeApi("PUT", `/api/squads/${squad.id}`, {
        instructions: "Engineer implements and opens a PR; Reviewer checks the diff; Docs updates reference pages before handoff.",
      });
      await safeApi("POST", `/api/squads/${squad.id}/members`, { member_type: "agent", member_id: reviewer.id, role: "member" });
      await safeApi("POST", `/api/squads/${squad.id}/members`, { member_type: "agent", member_id: docsAgent.id, role: "member" });
    }
  }

  // Quick Action
  const existingQA = (await safeApi("GET", "/api/quick-actions")) ?? [];
  const qaList = Array.isArray(existingQA) ? existingQA : existingQA.quick_actions ?? [];
  let quickAction = qaList.find((q) => q.name === "PR review");
  if (!quickAction) {
    quickAction = await safeApi("POST", "/api/quick-actions", {
      name: "PR review",
      description: "Ask Reviewer to inspect the linked pull request for regressions.",
      assignee_type: "agent",
      assignee_id: reviewer.id,
      prompt: "Review the linked pull request for edge cases, missing tests, and API compatibility.",
      visibility: "public",
    });
  }

  // Autopilot
  const existingAP = (await safeApi("GET", "/api/autopilots")) ?? [];
  const apList = Array.isArray(existingAP) ? existingAP : existingAP.autopilots ?? [];
  let autopilot = apList.find((a) => a.title === "Weekly dead-code audit");
  if (!autopilot) {
    autopilot = await safeApi("POST", "/api/autopilots", {
      title: "Weekly dead-code audit",
      description: "Scan packages/ and server/ for unused exports, stale feature flags, and outdated dependencies.",
      assignee_type: "agent",
      assignee_id: reviewer.id,
      execution_mode: "create_issue",
      issue_title_template: "Weekly audit · {{date}}",
    });
    if (autopilot?.id) {
      await safeApi("POST", `/api/autopilots/${autopilot.id}/triggers`, {
        kind: "schedule",
        cron_expression: "0 9 * * 1",
        timezone: "UTC",
        label: "Mondays at 09:00 UTC",
      });
    }
  }

  // Project
  const existingProjects = (await safeApi("GET", "/api/projects")) ?? [];
  const projList = Array.isArray(existingProjects) ? existingProjects : existingProjects.projects ?? [];
  let project = projList.find((p) => p.title === "API reliability") ?? projList[0];
  if (!project) {
    project = await api("POST", "/api/projects", {
      title: "API reliability",
      description: "Keep p95 claim latency under 10s and enforce rate limits across public endpoints.",
      status: "in_progress",
      priority: "high",
    });
  } else if (!project.description) {
    await safeApi("PUT", `/api/projects/${project.id}`, {
      description: "Keep p95 claim latency under 10s and enforce rate limits across public endpoints.",
    });
  }

  // Workflow project
  let wfProject = projList.find((p) => p.title === "Release Pipeline");
  if (!wfProject) {
    wfProject = await api("POST", "/api/projects", {
      title: "Release Pipeline",
      description: "Structured engineering handoffs from implementation to automated review and QA.",
      status: "in_progress",
      priority: "medium",
    });
  }
  const wfsRaw = (await safeApi("GET", "/api/issue-workflows")) ?? [];
  const wfs = Array.isArray(wfsRaw) ? wfsRaw : wfsRaw.workflows ?? [];
  let wf = wfs.find((w) => w.name === "Engineering Delivery");
  if (!wf) {
    wf = await safeApi("POST", "/api/issue-workflows", {
      name: "Engineering Delivery",
      description: "Engineer implements in In Progress, Reviewer audits in In Review.",
      steps: [
        { status_key: "todo", handler: { type: "none" } },
        {
          status_key: "in_progress",
          handler: { type: "agent", id: engineer.id },
          instructions: "Implement the change with unit tests, then move to In Review.",
          next_status_key: "in_review",
        },
        {
          status_key: "in_review",
          handler: { type: "agent", id: reviewer.id },
          instructions: "Review the diff and tests, then hand off for final sign-off.",
          next_status_key: "done",
          back_status_key: "in_progress",
        },
        { status_key: "done", handler: { type: "none" } },
        { status_key: "cancelled", handler: { type: "none" } },
      ],
    });
  }
  if (wf?.id && wfProject?.id) {
    await safeApi("POST", `/api/projects/${wfProject.id}/workflow`, { workflow_id: wf.id });
  }

  // Seed rich deliverable issue
  const deliverableIssue = await api("POST", "/api/issues", {
    title: "Benchmark claim latency across all five runtimes",
    description: "Measure p50/p95 claim latency and queue depth across our five build runtimes, then attach a summary table and fix diff.",
    status: "in_review",
    priority: "high",
    project_id: project?.id,
    allow_duplicate: true,
  });
  await api("POST", `/api/issues/${deliverableIssue.id}/comments`, {
    content: `Here is the 24-hour runtime comparison. **cloud-pool-b** is the outlier (p95 2.7× fleet median).\n\n| Runtime | Provider | p50 | p95 | Queue depth |\n| --- | --- | ---: | ---: | ---: |\n| build-mac-01 | Claude Code | 1.8 s | 7.4 s | 3 |\n| cloud-pool-a | Claude Code | 1.2 s | 4.9 s | 0 |\n| cloud-pool-b | Gemini CLI | 2.6 s | 11.3 s | 12 |\n\n\`\`\`diff\n--- a/server/internal/daemon/prepare.go\n+++ b/server/internal/daemon/prepare.go\n@@ -112,4 +112,4 @@\n-  p.client.ExtendPrepareLease(ctx, task.RuntimeID, task.ID)\n   p.cache.CopyInto(ctx, task.WorkDir, opts)\n+  p.client.ExtendPrepareLease(ctx, task.RuntimeID, task.ID)\n\`\`\`\n\nMoving lease extension after dependency copy cuts p95 from **11.3 s → 4.1 s**.`,
  });

  // Clean any prior placeholder seed titles so the sidebar always reads like a real workspace
  psql("DELETE FROM inbox_item WHERE details::text LIKE '%Analytics seed%';");
  psql("UPDATE issue SET title = 'Audit webhook retry backoff timings' WHERE title LIKE 'Analytics seed%';");

  // Seed realistic token usage over last 6 days so /usage chart is rich & populated
  const usageRows = [
    { agent: engineer, title: "Add request ID correlation to gateway logs", inTok: 48200, outTok: 9400, cost: 0.31, daysAgo: 5 },
    { agent: reviewer, title: "Review OAuth PKCE state validation PR", inTok: 31500, outTok: 5200, cost: 0.19, daysAgo: 4 },
    { agent: engineer, title: "Tune webhook exponential backoff limits", inTok: 62400, outTok: 12100, cost: 0.42, daysAgo: 3 },
    { agent: docsAgent, title: "Update OpenAPI spec for /v2/search", inTok: 24800, outTok: 6400, cost: 0.17, daysAgo: 2 },
    { agent: engineer, title: "Fix stale cache invalidation on issue moves", inTok: 79100, outTok: 14800, cost: 0.54, daysAgo: 1 },
    { agent: reviewer, title: "Audit SQL migration lock safety", inTok: 41200, outTok: 7300, cost: 0.27, daysAgo: 0 },
  ];
  const existingUsageCount = Number(psql("SELECT count(*) FROM task_usage;") || "0");
  if (existingUsageCount < 4) {
    for (const row of usageRows) {
      const iss = await api("POST", "/api/issues", {
        title: row.title,
        description: "Completed run with usage metrics.",
        status: "todo",
        priority: "low",
        assignee_type: "agent",
        assignee_id: row.agent.id,
        allow_duplicate: true,
      });
      const t = await claimTaskMatching((x) => x.issue_id === iss.id, 4000);
      if (t) {
        await startTask(t.id);
        await reportUsage(t.id, row.inTok, row.outTok, row.cost);
        await completeTask(t.id, "Completed.");
        if (row.daysAgo > 0) {
          psql(`UPDATE agent_task_queue SET created_at = NOW() - INTERVAL '${row.daysAgo} days', started_at = NOW() - INTERVAL '${row.daysAgo} days', completed_at = NOW() - INTERVAL '${row.daysAgo} days' + INTERVAL '42 seconds' WHERE id = '${t.id}';`);
          psql(`UPDATE task_usage SET created_at = NOW() - INTERVAL '${row.daysAgo} days' WHERE task_id = '${t.id}';`);
        }
      }
    }
  }
  clearStaleQueuedTasks();

  return { me, engineer, reviewer, docsAgent, tddSkill, squad, quickAction, autopilot, project, wfProject, deliverableIssue };
}

// ---------------------------------------------------------------------------
// 19 Scenarios
// ---------------------------------------------------------------------------
function buildScenarios(ctx) {
  return {
    assign: async (browser, theme) => {
      clearStaleQueuedTasks();
      const issue = await api("POST", "/api/issues", {
        title: "Add sliding-window rate limiting to /v2/search",
        description: "Enforce 120 req/min per API key on `/v2/search` and return `Retry-After` headers when exceeded.",
        status: "todo",
        priority: "high",
        project_id: ctx.project?.id,
        allow_duplicate: true,
      });
      return recordClip(browser, "assign", theme, `/issues/${issue.identifier}`, async ({ page, mark, setPoster }) => {
        mark("pick");
        await sleep(450);
        await clickButtonInZone(page, /Unassigned|Assignee/i, { minX: 860, pauseAfter: 450 });
        await clickPopoverItem(page, /^Engineer$/, { pauseAfter: 450 });

        mark("confirm");
        const confirmBtn = page.getByRole("button", { name: /Confirm assignment/i });
        if (await confirmBtn.isVisible({ timeout: 2000 }).catch(() => false)) {
          await sleep(450);
          await smoothClick(page, confirmBtn, { pauseAfter: 450 });
        }

        mark("running");
        const task = await claimTaskMatching((t) => t.issue_id === issue.id, 6000);
        if (task) {
          const asAgent = task.auth_token ? { Authorization: `Bearer ${task.auth_token}` } : {};
          await startTask(task.id);
          await reportMessage(task.id, { type: "text", content: "Checking current rate limiter middleware in server/internal/middleware." });
          await api("PUT", `/api/issues/${issue.id}`, { status: "in_progress" }, asAgent);
          await sleep(1000);

          mark("done");
          await api("POST", `/api/issues/${issue.id}/comments`, {
            content: "Implemented sliding-window rate limiting (120 req/min per key) with `Retry-After` headers and unit tests. Ready for review.",
          }, asAgent);
          await api("PUT", `/api/issues/${issue.id}`, { status: "in_review" }, asAgent);
          await completeTask(task.id, "Implemented sliding-window rate limiting on /v2/search.");
          await reportUsage(task.id, 21400, 4100, 0.16);
          await sleep(1100);
          setPoster();
        } else {
          setPoster();
          await sleep(900);
        }
      });
    },

    mention: async (browser, theme) => {
      clearStaleQueuedTasks();
      const issue = await api("POST", "/api/issues", {
        title: "Investigate intermittent 502s on EU webhook proxy",
        description: "EU customers saw brief 502 spikes during the 14:00 UTC deploy window.",
        status: "in_progress",
        priority: "high",
        assignee_type: "member",
        assignee_id: ctx.me.id,
        project_id: ctx.project?.id,
        allow_duplicate: true,
      });
      return recordClip(browser, "mention", theme, `/issues/${issue.identifier}`, async ({ page, mark, setPoster }) => {
        mark("compose");
        await focusAndTypeComment(page, "Please check proxy timeout logs @Reviewer");
        await sleep(300);
        await clickPopoverItem(page, /^Reviewer$/, { pauseAfter: 500 });

        mark("preview");
        setPoster();
        await sleep(750);
        await submitActiveComment(page);

        mark("reply");
        const task = await claimTaskMatching((t) => t.issue_id === issue.id, 5000);
        if (task) {
          const asAgent = task.auth_token ? { Authorization: `Bearer ${task.auth_token}` } : {};
          await startTask(task.id);
          await sleep(700);
          await api("POST", `/api/issues/${issue.id}/comments`, {
            content: "Checked nginx upstream logs: idle keepalive timeout was 60s vs ALB 65s, causing reset-on-reuse 502s. Assignee unchanged.",
          }, asAgent);
          await completeTask(task.id, "Diagnosed keepalive timeout mismatch.");
          await sleep(1050);
        } else {
          await sleep(750);
        }
      });
    },

    steer: async (browser, theme) => {
      clearStaleQueuedTasks();
      const issue = await api("POST", "/api/issues", {
        title: "Summarize last month's customer support tickets",
        description: "Review all 86 tickets from October and write an executive summary for Monday's standup.",
        status: "todo",
        priority: "medium",
        assignee_type: "agent",
        assignee_id: ctx.engineer.id,
        allow_duplicate: true,
      });
      const task = await claimTaskMatching((t) => t.issue_id === issue.id, 6000);
      let asAgent = {};
      if (task) {
        asAgent = task.auth_token ? { Authorization: `Bearer ${task.auth_token}` } : {};
        await startTask(task.id);
        await api("PUT", `/api/issues/${issue.id}`, { status: "in_progress" }, asAgent);
        await reportMessage(task.id, { type: "text", content: "Reading 86 tickets one by one and drafting per-ticket summaries (4/86)..." });
      }
      return recordClip(browser, "steer", theme, `/issues/${issue.identifier}`, async ({ page, mark, setPoster }) => {
        mark("working");
        await sleep(850);

        mark("steer");
        await focusAndTypeComment(page, "Skip per-ticket summaries — group into 3 themes with 2 quotes each, one page max.");
        setPoster();
        await sleep(600);
        await submitActiveComment(page);

        mark("redirected");
        if (task) {
          await reportMessage(task.id, { type: "text", content: "Received steering note — switching to 3-theme executive summary." });
          await sleep(750);
          await api("POST", `/api/issues/${issue.id}/comments`, {
            content: "Grouped all 86 tickets into 3 themes (Onboarding, Webhook retries, Pricing) with 2 customer quotes each — condensed to one page.",
          }, asAgent);
          await api("PUT", `/api/issues/${issue.id}`, { status: "in_review" }, asAgent);
          await completeTask(task.id, "Delivered 1-page themed summary.");
          await sleep(1050);
        }
      });
    },

    inbox: async (browser, theme) => {
      return recordClip(browser, "inbox", theme, "/issues", async ({ page, mark, setPoster }) => {
        mark("unread");
        await sleep(500);
        // Click first notification row under Inbox in Work panel (left sidebar x: 60..300, y: 260..520)
        const rows = page.locator("a, button, [role='button']").filter({
          hasText: /sliding-window|scroll position|Retry webhook|Summarize last month|Native menu/i,
        });
        const n = await rows.count();
        for (let i = 0; i < n; i++) {
          const el = rows.nth(i);
          if (!(await el.isVisible().catch(() => false))) continue;
          const box = await el.boundingBox();
          if (box && box.x >= 50 && box.x < 300 && box.y >= 260 && box.y < 520) {
            await clickAt(page, Math.round(box.x + box.width / 2), Math.round(box.y + box.height / 2), { pauseAfter: 900 });
            await page.getByText("Activity").first().waitFor({ state: "visible", timeout: 3000 }).catch(() => {});
            await sleep(450);
            break;
          }
        }
        setPoster();

        mark("archive");
        await clickButtonInZone(page, /Archive/i, { minX: 800, maxY: 160, pauseAfter: 750 });

        mark("archived");
        await clickButtonInZone(page, /Archived/i, { minX: 50, maxX: 340, minY: 150, maxY: 320, pauseAfter: 850 });
      });
    },

    workflow: async (browser, theme) => {
      clearStaleQueuedTasks();
      const issue = await api("POST", "/api/issues", {
        title: "Ship idempotency keys on POST /v1/invoices",
        description: "Ensure retried invoice requests return the original response safely.",
        status: "in_progress",
        priority: "high",
        project_id: ctx.wfProject?.id ?? ctx.project?.id,
        assignee_type: "member",
        assignee_id: ctx.me.id,
        allow_duplicate: true,
      });
      return recordClip(browser, "workflow", theme, `/issues/${issue.identifier}`, async ({ page, mark, setPoster }) => {
        mark("picker");
        await sleep(450);
        await clickButtonInZone(page, /In Progress/i, { minX: 860, maxY: 220, pauseAfter: 550 });
        setPoster();

        mark("handoff");
        await clickPopoverItem(page, /In Review/i, { pauseAfter: 500 });
        const confirmBtn = page.getByRole("button", { name: /Move to|Confirm/i }).first();
        if (await confirmBtn.isVisible({ timeout: 2000 }).catch(() => false)) {
          await sleep(450);
          await smoothClick(page, confirmBtn, { pauseAfter: 450 });
        }

        mark("next");
        const task = await claimTaskMatching((t) => t.issue_id === issue.id, 4500);
        if (task) {
          const asAgent = task.auth_token ? { Authorization: `Bearer ${task.auth_token}` } : {};
          await startTask(task.id);
          await sleep(700);
          await api("POST", `/api/issues/${issue.id}/comments`, {
            content: "Reviewed idempotency key store and TTL tests — no regressions found. Ready to ship.",
          }, asAgent);
          await completeTask(task.id, "Review passed.");
          await sleep(1000);
        } else {
          await sleep(800);
        }
      });
    },

    "issue-status": async (browser, theme) => {
      const issue = await api("POST", "/api/issues", {
        title: "Add CSV export to the invoices table",
        description: "Allow billing admins to download filtered monthly invoices as UTF-8 CSV.",
        status: "backlog",
        priority: "medium",
        project_id: ctx.project?.id,
        allow_duplicate: true,
      });
      return recordClip(browser, "issue-status", theme, `/issues/${issue.identifier}`, async ({ page, mark, setPoster }) => {
        mark("backlog");
        await sleep(450);
        await clickButtonInZone(page, /Backlog/i, { minX: 860, maxY: 220, pauseAfter: 450 });
        await clickPopoverItem(page, /^In Progress$/, { pauseAfter: 650 });

        mark("progress");
        setPoster();
        await clickButtonInZone(page, /In Progress/i, { minX: 860, maxY: 220, pauseAfter: 450 });
        await clickPopoverItem(page, /^In Review$/, { pauseAfter: 650 });

        mark("done");
        await clickButtonInZone(page, /In Review/i, { minX: 860, maxY: 220, pauseAfter: 450 });
        await clickPopoverItem(page, /^Done$/, { pauseAfter: 850 });
      });
    },

    "issue-draft": async (browser, theme) => {
      return recordClip(browser, "issue-draft", theme, "/issues", async ({ page, mark, setPoster }) => {
        mark("open");
        await sleep(450);
        await page.keyboard.press("c");
        await sleep(600);

        mark("draft");
        const input = page.locator('[role="dialog"] textarea, [role="dialog"] [contenteditable="true"], [role="dialog"] input[type="text"]').first();
        if (await input.isVisible({ timeout: 2500 }).catch(() => false)) {
          await smoothClick(page, input, { pauseAfter: 120 });
          await page.keyboard.type("Add dark mode contrast check to CI and fail if any token drops below WCAG AA.", { delay: 14 });
          setPoster();
          await sleep(500);
        }

        mark("create");
        const submit = page.locator('[role="dialog"]').getByRole("button", { name: /Create|Submit|Send/i }).last();
        await trySmoothClick(page, submit, { pauseAfter: 1050 });
      });
    },

    deliverables: async (browser, theme) => {
      return recordClip(browser, "deliverables", theme, `/issues/${ctx.deliverableIssue.identifier}`, async ({ page, mark, setPoster }) => {
        mark("overview");
        await sleep(650);
        setPoster();

        mark("table");
        await page.mouse.move(620, 420);
        await page.mouse.wheel(0, 260);
        await sleep(950);

        mark("diff");
        await page.mouse.wheel(0, 260);
        await sleep(1050);
      });
    },

    "resolve-thread": async (browser, theme) => {
      const issue = await api("POST", "/api/issues", {
        title: "Choose pagination cursor format for /v2/issues",
        description: "Decide between opaque base64 cursors and offset tokens before freezing the v2 API spec.",
        status: "in_review",
        priority: "medium",
        allow_duplicate: true,
      });
      const root = await api("POST", `/api/issues/${issue.id}/comments`, {
        content: "Should `/v2/issues` use opaque base64 cursors or numeric offsets?",
      });
      await api("POST", `/api/issues/${issue.id}/comments`, {
        content: "Opaque base64 keyset cursors avoid skipped rows when issues move between statuses under concurrent writes.",
        parent_id: root.id,
      });
      await api("POST", `/api/issues/${issue.id}/comments`, {
        content: "Agreed — freezing keyset cursors in the v2 OpenAPI spec.",
        parent_id: root.id,
      });

      return recordClip(browser, "resolve-thread", theme, `/issues/${issue.identifier}`, async ({ page, mark, setPoster }) => {
        mark("thread");
        await sleep(650);
        setPoster();

        mark("resolve");
        // Hover the root comment so the action menu button becomes visible
        const rootText = page.getByText("opaque base64 cursors or numeric offsets", { exact: false }).first();
        const box = await rootText.boundingBox();
        if (box) await moveCursor(page, Math.round(box.x + 120), Math.round(box.y));
        await sleep(250);
        await clickButtonInZone(page, /Comment actions|More/i, { minX: 360, maxX: 900, minY: 280, maxY: 600, pauseAfter: 450 });
        await clickPopoverItem(page, /Resolve thread/i, { pauseAfter: 850 });

        mark("collapsed");
        await sleep(850);
      });
    },

    chat: async (browser, theme) => {
      clearStaleQueuedTasks();
      const session = await api("POST", "/api/chat/sessions", {
        agent_id: ctx.engineer.id,
        title: "Release readiness check",
      });
      return recordClip(browser, "chat", theme, `/chat?session=${encodeURIComponent(session.id)}`, async ({ page, mark, setPoster }) => {
        mark("prompt");
        await sleep(450);
        const box = page.locator('textarea, [contenteditable="true"]').last();
        await smoothClick(page, box, { pauseAfter: 120 });
        await page.keyboard.type("List our open high-priority issues and flag any blockers before v0.13.", { delay: 14 });
        await sleep(250);
        // Click the round send button at bottom-right of chat composer
        const sendBtn = page.locator('button[aria-label*="Send" i], button[type="submit"]').last();
        if (!(await trySmoothClick(page, sendBtn, { pauseAfter: 450 }))) {
          await page.keyboard.press("Meta+Enter");
          await sleep(450);
        }

        mark("thinking");
        const task = await claimTaskMatching(() => true, 5000);
        if (task) {
          await startTask(task.id);
          await sleep(750);
          mark("reply");
          await completeTask(
            task.id,
            `Three high-priority issues remain before **v0.13**:\n\n| Issue | Status | Note |\n| --- | --- | --- |\n| Rate-limit \`/v2/search\` | In Review | PR checks green |\n| Retry webhook deliveries | Todo | Needs backoff cap decision |\n| Slow claims on \`cloud-pool-b\` | In Progress | Cache pre-warm patch in testing |`,
          );
          await sleep(1100);
          setPoster();
        } else {
          setPoster();
          await sleep(800);
        }
      });
    },

    "create-agent": async (browser, theme) => {
      return recordClip(browser, "create-agent", theme, "/agents", async ({ page, mark, setPoster }) => {
        mark("start");
        await sleep(500);
        const newBtn = page.getByRole("button", { name: /New agent|Create agent/i }).or(page.getByRole("link", { name: /New agent|Create agent/i })).first();
        await trySmoothClick(page, newBtn, { pauseAfter: 650 });

        mark("runtime");
        const rt = page.getByText(/Claude Code|Maya's MacBook Pro/i).first();
        await trySmoothClick(page, rt, { pauseAfter: 450 });
        await trySmoothClick(page, page.getByRole("button", { name: /Next|Continue/i }).first(), { pauseAfter: 600 });
        setPoster();

        mark("configure");
        const nameInput = page.locator('input[placeholder*="name" i], input[name="name"]').first();
        if (await nameInput.isVisible({ timeout: 2000 }).catch(() => false)) {
          await smoothClick(page, nameInput, { pauseAfter: 120 });
          await page.keyboard.type("Release QA", { delay: 20 });
          await sleep(600);
        } else {
          await sleep(750);
        }
      });
    },

    project: async (browser, theme) => {
      return recordClip(browser, "project", theme, `/projects/${ctx.project.id}`, async ({ page, mark, setPoster }) => {
        mark("overview");
        await sleep(750);
        setPoster();

        mark("issues");
        const issuesTab = page.getByRole("tab", { name: /Issues/i }).or(page.getByRole("button", { name: /Issues/i })).first();
        await trySmoothClick(page, issuesTab, { pauseAfter: 850 });

        mark("resources");
        const resTab = page.getByRole("tab", { name: /Overview|Activity/i }).or(page.getByRole("button", { name: /Overview/i })).first();
        await trySmoothClick(page, resTab, { pauseAfter: 850 });
      });
    },

    skill: async (browser, theme) => {
      return recordClip(browser, "skill", theme, "/skills", async ({ page, mark, setPoster }) => {
        mark("list");
        await sleep(600);
        await trySmoothClick(page, page.getByText("tdd", { exact: true }).first(), { pauseAfter: 850 });

        mark("detail");
        setPoster();
        await sleep(850);

        mark("mounted");
        await page.mouse.wheel(0, 220);
        await sleep(850);
      });
    },

    squad: async (browser, theme) => {
      return recordClip(browser, "squad", theme, "/squads", async ({ page, mark, setPoster }) => {
        mark("list");
        await sleep(600);
        await trySmoothClick(page, page.getByText("Platform Team", { exact: false }).first(), { pauseAfter: 850 });

        mark("detail");
        setPoster();
        await sleep(900);

        mark("workflow");
        await page.mouse.wheel(0, 220);
        await sleep(850);
      });
    },

    autopilot: async (browser, theme) => {
      return recordClip(browser, "autopilot", theme, "/autopilots", async ({ page, mark, setPoster }) => {
        mark("list");
        await sleep(600);
        await trySmoothClick(page, page.getByText("Weekly dead-code audit", { exact: false }).first(), { pauseAfter: 850 });

        mark("detail");
        setPoster();
        await sleep(750);

        mark("run");
        await trySmoothClick(page, page.getByRole("button", { name: /Run now/i }).first(), { pauseAfter: 950 });
      });
    },

    wakeup: async (browser, theme) => {
      clearStaleQueuedTasks();
      const issue = await api("POST", "/api/issues", {
        title: "Verify canary error rate 1 hour after deploy",
        description: "Check p99 latency and 5xx rate once EU canary has served traffic for an hour.",
        status: "in_progress",
        priority: "medium",
        assignee_type: "member",
        assignee_id: ctx.me.id,
        allow_duplicate: true,
      });
      return recordClip(browser, "wakeup", theme, `/issues/${issue.identifier}`, async ({ page, mark, setPoster }) => {
        mark("sidebar");
        await sleep(550);
        const wakeHeader = page.getByText("Wakeups", { exact: true }).first();
        const addWakeBtn = wakeHeader.locator("xpath=ancestor::div[1]").locator("button").last();
        if (!(await trySmoothClick(page, addWakeBtn, { pauseAfter: 650 }))) {
          const box = await wakeHeader.boundingBox();
          if (box) {
            await clickAt(page, 1236, Math.round(box.y + box.height / 2), { pauseAfter: 650 });
          }
        }
        setPoster();

        mark("configure");
        const preset = page.getByRole("button", { name: /In 1 hour|In 10 minutes|At a time|Time/i }).first();
        await trySmoothClick(page, preset, { pauseAfter: 600 });

        mark("saved");
        const saveBtn = page.getByRole("button", { name: /Add wakeup|Create|Save/i }).last();
        await trySmoothClick(page, saveBtn, { pauseAfter: 850 });
      });
    },

    "quick-action": async (browser, theme) => {
      clearStaleQueuedTasks();
      const issue = await api("POST", "/api/issues", {
        title: "Refactor OAuth token refresh locking",
        description: "Prevent thundering herd refresh calls when multiple tabs wake from sleep.",
        status: "in_review",
        priority: "high",
        allow_duplicate: true,
      });
      return recordClip(browser, "quick-action", theme, `/issues/${issue.identifier}`, async ({ page, mark, setPoster }) => {
        mark("sidebar");
        await sleep(600);
        setPoster();

        mark("trigger");
        await clickButtonInZone(page, /PR review/i, { minX: 860, pauseAfter: 700 });

        mark("run");
        const sendBtn = page.getByRole("button", { name: /Run|Send|Confirm/i }).last();
        await trySmoothClick(page, sendBtn, { pauseAfter: 900 });
      });
    },

    "pr-link": async (browser, theme) => {
      const issue = await api("POST", "/api/issues", {
        title: "Highlight actionable UI areas in tutorial screenshots",
        description: "Add soft spotlight crops and numbered badges to Complete tutorial screenshots.",
        status: "in_review",
        priority: "medium",
        project_id: ctx.project?.id,
        allow_duplicate: true,
      });
      // Seed a real synced GitHub PR row + link in the isolated local dev DB
      const prId = psql(`
        INSERT INTO github_pull_request (
          workspace_id, installation_id, repo_owner, repo_name, pr_number,
          title, state, html_url, branch, author_login,
          pr_created_at, pr_updated_at, head_sha, mergeable_state,
          additions, deletions, changed_files
        ) VALUES (
          '${profile.workspace_id}', 1001, 'multica-ai', 'multica', 11306,
          'Highlight actionable UI areas in tutorial screenshots', 'open',
          'https://github.com/multica-ai/multica/pull/11306', 'feat/tutorial-spotlights', 'engineer-bot',
          NOW() - INTERVAL '2 hours', NOW() - INTERVAL '5 minutes', '665b4cd8e1a', 'clean',
          184, 42, 6
        )
        ON CONFLICT (workspace_id, repo_owner, repo_name, pr_number) DO UPDATE SET
          title = EXCLUDED.title, state = EXCLUDED.state
        RETURNING id;
      `).split("\n")[0].trim();
      if (prId) {
        psql(`
          INSERT INTO issue_pull_request (issue_id, pull_request_id, linked_by_type)
          VALUES ('${issue.id}', '${prId}', 'member')
          ON CONFLICT DO NOTHING;
        `);
      }

      return recordClip(browser, "pr-link", theme, `/issues/${issue.identifier}`, async ({ page, mark, setPoster }) => {
        mark("panel");
        await sleep(750);
        setPoster();

        mark("add");
        const prHeader = page.getByText("Pull requests", { exact: true }).first();
        const box = await prHeader.boundingBox();
        if (box) await clickAt(page, 1205, Math.round(box.y + box.height / 2), { pauseAfter: 750 });

        mark("synced");
        await page.keyboard.press("Escape");
        await sleep(750);
      });
    },

    usage: async (browser, theme) => {
      return recordClip(browser, "usage", theme, "/usage", async ({ page, mark, setPoster }) => {
        mark("overview");
        await sleep(750);
        setPoster();

        mark("metrics");
        await clickButtonInZone(page, /^Cost$/, { minX: 700, maxY: 420, pauseAfter: 800 });

        mark("errors");
        await clickButtonInZone(page, /^Errors$/, { minX: 300, maxX: 620, maxY: 90, pauseAfter: 850 });
      });
    },
  };
}

function writeManifest(manifest) {
  writeFileSync(manifestJsonPath, `${JSON.stringify(manifest, null, 2)}\n`);
  const ts = `// Auto-generated by scripts/docs-demos/record.mjs — do not edit by hand.
export interface ClipManifestEntry {
  duration: number;
  chapters: { id: string; at: number }[];
}

export const CLIP_MANIFEST: Record<string, ClipManifestEntry> = ${JSON.stringify(manifest, null, 2)};
`;
  writeFileSync(manifestTsPath, ts);
}

async function main() {
  const existingManifest = existsSync(manifestJsonPath)
    ? JSON.parse(readFileSync(manifestJsonPath, "utf8"))
    : {};
  const shared = await seedShared();
  const scenarios = buildScenarios(shared);
  const browser = await chromium.launch();

  try {
    const names = Object.keys(scenarios).filter((n) => !onlySet || onlySet.has(n));
    for (const name of names) {
      log(`=== Recording ${name} ===`);
      const light = await scenarios[name](browser, "light");
      const dark = await scenarios[name](browser, "dark");
      existingManifest[name] = {
        duration: light.duration,
        chapters: light.marks,
      };
      writeManifest(existingManifest);
      void dark;
    }
  } finally {
    if (heartbeatTimer) clearInterval(heartbeatTimer);
    await browser.close();
  }

  log(`Done! Manifest written to ${manifestTsPath}`);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
