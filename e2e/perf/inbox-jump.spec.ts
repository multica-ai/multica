/**
 * Inbox → deep comment on a long issue, cold and warm, against a real local API.
 * Opt in with PERF_SCENARIO=inbox-jump. See README "Inbox jump".
 */
import { test, expect, type Browser, type Page } from "@playwright/test";
import { createHash } from "node:crypto";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { loadavg } from "node:os";
import pg from "pg";
import { createTestApi } from "../helpers";

interface Manifest {
  version: number;
  digest: string;
  token: string;
  workspaceSlug: string;
  issueId: string;
  identifier: string;
  title: string;
  targetId: string;
  neighbourTitle: string;
  counts: { comments: number; replies: number; runs: number; children: number; activities: number };
}

const DATASET_VERSION = 2;

/** Deterministic PRNG so every seeding produces the same issue. */
function rng(seed: number) {
  let s = seed >>> 0;
  return () => {
    s = (s + 0x6d2b79f5) >>> 0;
    let t = s;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const WORDS = "the timeline issue comment run agent review markdown virtual list mount measure height query cache prefetch render layout style frame task thread reply inbox anchor scroll target viewport budget".split(" ");

function sentence(r: () => number, n: number) {
  const out: string[] = [];
  for (let i = 0; i < n; i++) out.push(WORDS[Math.floor(r() * WORDS.length)]!);
  const s = out.join(" ");
  return s[0]!.toUpperCase() + s.slice(1) + ".";
}

function paragraph(r: () => number) {
  return Array.from({ length: 2 + Math.floor(r() * 4) }, () => sentence(r, 8 + Math.floor(r() * 10))).join(" ");
}

/** An agent report: headings, a table, lists, code and inline code. */
function agentReport(r: () => number, i: number) {
  const rows = 3 + Math.floor(r() * 6);
  const table = ["| Scenario | Metric | Before | After |", "|---|---|---|---|",
    ...Array.from({ length: rows }, (_, j) => `| Case ${j} | \`metric_${j}\` | ${Math.floor(r() * 2000)}ms | ${Math.floor(r() * 900)}ms |`)].join("\n");
  const code = ["```ts", ...Array.from({ length: 4 + Math.floor(r() * 14) }, (_, j) =>
    `const value${j} = await api.listTimeline(issueId, { cursor: "${Math.floor(r() * 1e6).toString(36)}" }); // ${sentence(r, 4)}`), "```"].join("\n");
  const list = Array.from({ length: 3 + Math.floor(r() * 5) }, () => `- **${sentence(r, 2)}** ${sentence(r, 10)} See https://github.com/multica-ai/multica/pull/${10000 + Math.floor(r() * 900)}.`).join("\n");
  return [`Report ${i}: ${sentence(r, 6)}`, `## Findings`, paragraph(r), paragraph(r), list, `## Numbers`, table, paragraph(r),
    `## Change`, code, paragraph(r), paragraph(r), `## Trade-offs`, paragraph(r), list, paragraph(r),
    `## Verification`, Array.from({ length: 5 }, (_, j) => `${j + 1}. ${sentence(r, 14)}`).join("\n"), paragraph(r)].join("\n\n");
}

function humanComment(r: () => number, i: number) {
  return `Comment ${i}: ${Array.from({ length: 2 + Math.floor(r() * 4) }, () => paragraph(r)).join("\n\n")}`;
}

async function seed(manifestPath: string): Promise<Manifest> {
  const api = await createTestApi();
  const db = new pg.Client(process.env.DATABASE_URL);
  await db.connect();
  try {
    const r = rng(9538);
    const workspace = (await api.getWorkspaces())[0]!;
    api.setWorkspaceId(workspace.id);
    const userId = (await db.query('SELECT id FROM "user" WHERE email=$1', [api.getEmail()])).rows[0].id as string;
    const runtimeId = (await db.query(`INSERT INTO agent_runtime
      (workspace_id,name,runtime_mode,provider,status,device_info,metadata,owner_id)
      VALUES ($1,'Benchmark runtime','cloud','test','offline','Test','{}',$2) RETURNING id`, [workspace.id, userId])).rows[0].id as string;
    const agentId = (await db.query(`INSERT INTO agent
      (workspace_id,name,description,instructions,runtime_mode,runtime_config,runtime_id,visibility,permission_mode,max_concurrent_tasks,owner_id)
      VALUES ($1,'Benchmark agent','','','cloud','{}',$2,'workspace','private',1,$3) RETURNING id`, [workspace.id, runtimeId, userId])).rows[0].id as string;

    await db.query("DELETE FROM inbox_item WHERE workspace_id=$1 AND title LIKE 'Inbox jump benchmark%'", [workspace.id]);
    const title = "Inbox jump benchmark: long issue";
    const issue = await api.createIssue(title, {
      description: Array.from({ length: 6 }, () => paragraph(r)).join("\n\n"),
      status: "in_progress",
    });
    for (let i = 0; i < 24; i++) {
      await api.createIssue(`Benchmark sub-issue ${i}`, {
        parent_issue_id: issue.id,
        status: (["todo", "in_progress", "in_review", "done"] as const)[i % 4],
        description: Array.from({ length: 2 + (i % 5) }, () => paragraph(r)).join("\n\n"),
      });
    }

    // 31 root comments + 18 replies = 49, interleaved with 20 activity rows,
    // spread over 72 runs; the target is a reply about two thirds down.
    const base = Date.UTC(2026, 9, 1);
    let t = 0;
    const at = () => new Date(base + (t += 60_000 + Math.floor(r() * 600_000))).toISOString();
    const insertComment = async (content: string, author: "member" | "agent", parentId: string | null, sourceTask: string | null) =>
      (await db.query(`INSERT INTO comment (workspace_id,issue_id,author_type,author_id,content,parent_id,source_task_id,created_at,updated_at)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$8) RETURNING id`,
      [workspace.id, issue.id, author, author === "agent" ? agentId : userId, content, parentId, sourceTask, at()])).rows[0].id as string;
    const insertRun = async () => {
      const completed = at();
      return (await db.query(`INSERT INTO agent_task_queue
        (agent_id,runtime_id,issue_id,status,result,created_at,dispatched_at,started_at,completed_at)
        VALUES ($1,$2,$3,'completed',$4,$5,$5,$5,$5) RETURNING id`,
      [agentId, runtimeId, issue.id, JSON.stringify({ comment: sentence(r, 12), output: Array.from({ length: 6 }, () => paragraph(r)).join("\n") }), completed])).rows[0].id as string;
    };
    const insertActivity = async (i: number) => {
      const statuses = ["todo", "in_progress", "in_review", "blocked"];
      await db.query(`INSERT INTO activity_log (workspace_id,issue_id,actor_type,actor_id,action,details,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
        [workspace.id, issue.id, i % 2 ? "agent" : "member", i % 2 ? agentId : userId,
          i % 3 === 0 ? "priority_changed" : "status_changed",
          JSON.stringify(i % 3 === 0 ? { from: "medium", to: "high" } : { from: statuses[i % 4], to: statuses[(i + 1) % 4] }), at()]);
    };

    let runs = 0;
    let replies = 0;
    let activities = 0;
    let targetId = "";
    for (let i = 0; i < 31; i++) {
      // Runs land between comments, as agents work between discussion turns.
      const runsHere = i === 30 ? 72 - runs : Math.min(72 - runs, 1 + Math.floor(r() * 3));
      let lastRun: string | null = null;
      for (let k = 0; k < runsHere; k++) { lastRun = await insertRun(); runs++; }
      if (i % 3 === 1 && activities < 20) { await insertActivity(activities++); }
      const agentAuthored = i % 2 === 1;
      const rootId = await insertComment(agentAuthored ? agentReport(r, i) : humanComment(r, i), agentAuthored ? "agent" : "member", null, agentAuthored ? lastRun : null);
      // 15 one-reply threads and one three-reply thread hold the 18 replies.
      const replyCount = i % 2 === 0 ? (i === 20 ? 3 : 1) : 0;
      for (let k = 0; k < replyCount; k++) {
        const agentReply = k % 2 === 1;
        const id = await insertComment(agentReply ? agentReport(r, 100 + replies) : humanComment(r, 100 + replies), agentReply ? "agent" : "member", rootId, null);
        replies++;
        if (i === 20 && k === 1) targetId = id;
      }
      if (i % 4 === 3 && activities < 20) { await insertActivity(activities++); }
    }
    while (activities < 20) await insertActivity(activities++);
    expect(targetId).not.toBe("");
    await api.seedCommentNotification(issue.id, targetId, title);

    // A small neighbour, so the warm case can switch away and back.
    const neighbourTitle = "Inbox jump benchmark: small issue";
    const neighbour = await api.createIssue(neighbourTitle, { description: paragraph(r) });
    const neighbourComment = await api.createComment(neighbour.id, humanComment(r, 999));
    await api.seedCommentNotification(neighbour.id, neighbourComment.id, neighbourTitle);

    const contents = (await db.query("SELECT content FROM comment WHERE issue_id=$1 ORDER BY created_at", [issue.id])).rows.map((row) => row.content);
    const manifest: Manifest = {
      version: DATASET_VERSION,
      digest: createHash("sha256").update(JSON.stringify(contents)).digest("hex").slice(0, 16),
      token: api.getToken()!, workspaceSlug: workspace.slug,
      issueId: issue.id, identifier: issue.identifier, title, targetId, neighbourTitle,
      counts: { comments: contents.length, replies, runs, children: 24, activities },
    };
    writeFileSync(manifestPath, JSON.stringify(manifest, null, 2));
    return manifest;
  } finally {
    await db.end();
  }
}

type Sample = Record<string, number | string | boolean | null>;

const PAGE_INSTRUMENTATION = () => {
  const w = window as unknown as { __bench: Record<string, unknown> };
  const state = {
    longTasks: [] as { start: number; duration: number }[],
    clickTs: 0,
    addedBodies: 0,
    addedPages: 0,
  };
  w.__bench = state;
  new PerformanceObserver((list) => {
    for (const e of list.getEntries()) state.longTasks.push({ start: e.startTime, duration: e.duration });
  }).observe({ type: "longtask", buffered: false });
  document.addEventListener("pointerdown", (e) => {
    state.clickTs = e.timeStamp;
    performance.mark("bench:click");
  }, { capture: true });
  const count = (node: Node) => {
    if (!(node instanceof Element)) return;
    state.addedBodies += node.matches('[data-comment-content]:not([data-comment-content^="description:"])') ? 1 : 0;
    state.addedBodies += node.querySelectorAll('[data-comment-content]:not([data-comment-content^="description:"])').length;
    state.addedPages += node.matches('[data-tab-scroll-root^="main:"]') ? 1 : 0;
    state.addedPages += node.querySelectorAll('[data-tab-scroll-root^="main:"]').length;
  };
  new MutationObserver((records) => {
    for (const record of records) record.addedNodes.forEach(count);
  }).observe(document, { childList: true, subtree: true });
};

/** Resolves (in page) with the first frame time at which the target is mounted and in view. */
const WATCH_TARGET = (targetId: string) => new Promise<number>((resolve) => {
  const check = () => {
    const container = document.querySelector<HTMLElement>('[data-tab-scroll-root^="main:"]');
    const target = document.getElementById(`comment-${targetId}`);
    if (container && target && target.getAttribute("data-virtual-thread-mounted") !== "false") {
      const c = container.getBoundingClientRect();
      const r = target.getBoundingClientRect();
      if (r.height > 0 && r.bottom > c.top + 20 && r.top < c.bottom - 20) {
        performance.mark("bench:visible");
        resolve(performance.now());
        return;
      }
    }
    requestAnimationFrame(check);
  };
  requestAnimationFrame(check);
});

async function resetCounters(page: Page) {
  await page.evaluate(() => {
    const s = (window as unknown as { __bench: { longTasks: unknown[]; addedBodies: number; addedPages: number; clickTs: number } }).__bench;
    s.longTasks.length = 0; s.addedBodies = 0; s.addedPages = 0; s.clickTs = 0;
  });
}

async function measureJump(page: Page, m: Manifest, label: string, settleMs: number) {
  await resetCounters(page);
  const visible = page.evaluate(WATCH_TARGET, m.targetId);
  await page.getByText(m.title, { exact: true }).first().click();
  const visibleTs = await visible;
  await page.waitForTimeout(settleMs);
  return page.evaluate(({ visibleTs, settleMs }) => {
    const s = (window as unknown as { __bench: { longTasks: { start: number; duration: number }[]; clickTs: number; addedBodies: number; addedPages: number } }).__bench;
    const inWindow = (end: number) => s.longTasks.filter((t) => t.start + t.duration > s.clickTs && t.start < end);
    const toVisible = inWindow(visibleTs);
    const settled = inWindow(visibleTs + settleMs);
    const sum = (list: { duration: number }[]) => list.reduce((a, t) => a + t.duration, 0);
    return {
      visible_ms: visibleTs - s.clickTs,
      longest_task_ms: Math.max(0, ...settled.map((t) => t.duration)),
      long_task_total_ms: sum(settled),
      long_task_total_to_visible_ms: sum(toVisible),
      bodies_added: s.addedBodies,
      pages_added: s.addedPages,
      bodies_mounted: document.querySelectorAll('[data-comment-content]:not([data-comment-content^="description:"])').length,
      dom_nodes: document.getElementsByTagName("*").length,
    };
  }, { visibleTs, settleMs }).then((x) => ({ ...x, case: label }));
}

async function openContext(browser: Browser, m: Manifest) {
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  await context.addInitScript((token) => {
    localStorage.setItem("multica_token", token);
    localStorage.setItem("multica:chat:isOpen", "false");
  }, m.token);
  await context.addInitScript(PAGE_INSTRUMENTATION);
  const page = await context.newPage();
  if (process.env.PERF_LATENCY_MS) {
    // Round-trip latency of a remote API; the local one answers in ~1ms.
    const cdp = await context.newCDPSession(page);
    await cdp.send("Network.enable");
    await cdp.send("Network.emulateNetworkConditions", {
      offline: false, latency: Number(process.env.PERF_LATENCY_MS), downloadThroughput: -1, uploadThroughput: -1,
    });
  }
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  return { context, page, errors };
}

/** One sample: cold jump from a loaded Inbox, then away to the neighbour and back (warm). */
async function markUnread(m: Manifest) {
  // Opening an issue reads its notification; each sample starts from a new, unread one.
  const db = new pg.Client(process.env.DATABASE_URL);
  await db.connect();
  try {
    await db.query("UPDATE inbox_item SET read=false WHERE issue_id=$1", [m.issueId]);
  } finally {
    await db.end();
  }
}

async function sampleOnce(browser: Browser, baseURL: string, m: Manifest, trace?: string) {
  await markUnread(m);
  const { context, page, errors } = await openContext(browser, m);
  try {
    await page.goto(`${baseURL}/${m.workspaceSlug}/inbox`);
    await expect(page.getByText(m.title, { exact: true }).first()).toBeVisible({ timeout: 30_000 });
    await expect(page.getByText(m.neighbourTitle, { exact: true }).first()).toBeVisible();
    // Let the Inbox settle (idle work, fonts) before the click, as a reader would.
    await page.waitForTimeout(Number(process.env.PERF_COLD_SETTLE_MS ?? 1500));
    const session = trace ? await context.newCDPSession(page) : null;
    const traceEvents: unknown[] = [];
    const startTrace = async () => {
      if (!session) return;
      traceEvents.length = 0;
      session.on("Tracing.dataCollected", (e) => traceEvents.push(...(e.value as unknown[])));
      await session.send("Tracing.start", {
        categories: "devtools.timeline,disabled-by-default-devtools.timeline,v8.execute,blink.user_timing,loading,v8",
        transferMode: "ReportEvents",
      } as never);
      await session.send("Profiler.enable");
      await session.send("Profiler.setSamplingInterval", { interval: 100 });
      await session.send("Profiler.start");
    };
    const stopTrace = async (path: string) => {
      if (!session) return;
      const { profile } = await session.send("Profiler.stop");
      writeFileSync(path.replace(/\.json$/, ".cpuprofile"), JSON.stringify(profile));
      const done = new Promise<void>((resolve) => session.once("Tracing.tracingComplete", () => resolve()));
      await session.send("Tracing.end");
      await done;
      writeFileSync(path, JSON.stringify({ traceEvents }));
    };
    await startTrace();
    const cold = await measureJump(page, m, "cold", 600);
    await stopTrace(`${trace}-cold.json`);
    // Away to the neighbour, then back.
    await page.getByText(m.neighbourTitle, { exact: true }).first().click();
    await expect(page.locator('[data-tab-scroll-root^="main:"]').getByText(/Comment 999/).first()).toBeVisible({ timeout: 30_000 });
    await page.waitForTimeout(1000);
    await startTrace();
    const warm = await measureJump(page, m, "warm", 600);
    await stopTrace(`${trace}-warm.json`);
    expect(errors).toEqual([]);
    return [cold, warm];
  } finally {
    await context.close();
  }
}

test("inbox jump to a deep comment, cold and warm", async ({ browser }) => {
  test.skip(process.env.PERF_SCENARIO !== "inbox-jump", "Requires an isolated local API and production builds");
  test.setTimeout(1_800_000);
  const manifestPath = process.env.PERF_MANIFEST ?? "inbox-jump-manifest.json";
  const manifest: Manifest = existsSync(manifestPath) ? JSON.parse(readFileSync(manifestPath, "utf8")) : await seed(manifestPath);
  console.log(JSON.stringify({ dataset: manifest.counts, digest: manifest.digest, identifier: manifest.identifier }));
  if (process.env.PERF_SEED_ONLY) return;

  // PERF_TARGETS=main=http://localhost:3101,head=http://localhost:3102 — rounds alternate between targets.
  const targets = (process.env.PERF_TARGETS ?? `head=${process.env.PLAYWRIGHT_BASE_URL ?? "http://localhost:3000"}`)
    .split(",").map((pair) => { const [name, url] = pair.split("="); return { name: name!, url: url! }; });
  const repeats = Number(process.env.PERF_REPEATS ?? 5);
  const traceDir = process.env.PERF_TRACE_DIR;
  const samples: Sample[] = [];
  for (let round = -1; round < repeats; round++) {
    for (const target of round % 2 ? targets : [...targets].reverse()) {
      const traced = traceDir && round >= 0 && (process.env.PERF_TRACE_ALL || round === repeats - 1);
      const trace = traced ? `${traceDir}/${target.name}-r${round}` : undefined;
      const results = await sampleOnce(browser, target.url, manifest, trace);
      if (round < 0) continue; // warmup
      for (const result of results) {
        const record = { target: target.name, round, load_1m: loadavg()[0]!, ...result };
        samples.push(record);
        console.log(JSON.stringify(record));
      }
    }
  }
  const median = (values: number[]) => {
    const sorted = [...values].sort((a, b) => a - b);
    const mid = Math.floor(sorted.length / 2);
    return sorted.length % 2 ? sorted[mid]! : (sorted[mid - 1]! + sorted[mid]!) / 2;
  };
  const summary = targets.flatMap((target) => (["cold", "warm"] as const).map((kind) => {
    const rows = samples.filter((s) => s.target === target.name && s.case === kind);
    const pick = (key: string) => median(rows.map((row) => Number(row[key])));
    return { target: target.name, case: kind, visible_ms: pick("visible_ms"), longest_task_ms: pick("longest_task_ms"), long_task_total_ms: pick("long_task_total_ms") };
  }));
  for (const row of summary) console.log(JSON.stringify({ median: row }));
  if (process.env.PERF_REPORT_PATH) {
    writeFileSync(process.env.PERF_REPORT_PATH, JSON.stringify({
      browser: browser.version(), viewport: "1440x900", cpu_throttle: 1, latency_ms: Number(process.env.PERF_LATENCY_MS ?? 0),
      dataset: manifest.counts, digest: manifest.digest, summary, samples,
    }, null, 2));
  }
  // Regression budget on the medians, enforced only when set (MUL-9538 targets:
  // warm ≤100ms with no task over 50ms, cold ≤300ms).
  const budget = (name: string) => (process.env[name] ? Number(process.env[name]) : null);
  for (const row of summary) {
    const visible = budget(row.case === "warm" ? "PERF_BUDGET_WARM_MS" : "PERF_BUDGET_COLD_MS");
    const longest = budget(row.case === "warm" ? "PERF_BUDGET_WARM_LONGEST_TASK_MS" : "PERF_BUDGET_COLD_LONGEST_TASK_MS");
    if (visible !== null) expect.soft(row.visible_ms, `${row.target} ${row.case}: target visible`).toBeLessThanOrEqual(visible);
    if (longest !== null) expect.soft(row.longest_task_ms, `${row.target} ${row.case}: longest task`).toBeLessThanOrEqual(longest);
  }
});
