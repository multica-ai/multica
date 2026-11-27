/**
 * One huge comment body on an otherwise ordinary issue (GitHub #11547).
 *
 * The other history scenarios grow the number of comments; none of their
 * bodies passes ~2,000 characters. The cost here grows with the size of a
 * single body instead, because every body runs through the full Markdown
 * pipeline when its thread mounts.
 *
 * Length alone is not what hurts. A multi-line log or prose with balanced
 * brackets parses in linear time; one long paragraph with an unclosed `[` near
 * its start (every JSON array export) does not, because GFM's autolink-literal
 * extension walks back to that bracket at every word
 * (micromark/micromark-extension-gfm-autolink-literal#13). The datasets keep
 * both shapes so a fix to either side shows up.
 *
 * Real API/database and production UI; opt in with PERF_SCENARIO=huge-comment.
 */
import { test, expect, type Page } from "@playwright/test";
import { createHash } from "node:crypto";
import { writeFileSync } from "node:fs";
import { loadavg } from "node:os";
import pg from "pg";
import { createTestApi } from "../helpers";

/** How long a sample may wait for the huge body before it counts as frozen. */
const READY_TIMEOUT_MS = Number(process.env.PERF_READY_TIMEOUT_MS ?? 60_000);

const SENTENCES = [
  "The timeline now loads the latest page first and fetches older entries on demand.",
  "I reproduced this locally with the large fixture; the second click is where it stalls.",
  "Agreed, but we should keep the reading position stable when older comments arrive.",
  "The query plan switched to a sequential scan once the table passed a few thousand rows.",
  "Let's ship the narrow fix first and follow up with the refactor in a separate PR.",
  "This only happens when the issue has more than a screenful of history.",
  "I checked the trace: most of the time goes to style recalculation after the list mounts.",
];

function rng(seed: number) {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const sentence = (rand: () => number) => SENTENCES[Math.floor(rand() * SENTENCES.length)]!;

function shortComment(i: number): string {
  const rand = rng(1000 + i);
  return `${sentence(rand)} ${sentence(rand)}\n\n${sentence(rand)}`;
}

/** A design note an agent might write: headings, prose, lists, code, tables, links. */
function prose(chars: number): string {
  const rand = rng(chars);
  const parts: string[] = [];
  let size = 0;
  for (let n = 0; size < chars; n++) {
    let part: string;
    if (n % 12 === 0) part = `## Section ${n / 12 + 1}`;
    else if (n % 12 === 4) part = Array.from({ length: 4 }, () => `- ${sentence(rand)}`).join("\n");
    else if (n % 12 === 7) part = "```ts\nconst page = await api.listTimeline(issueId, { before: cursor, limit: 50 });\nfor (const entry of page.entries) render(entry);\n```";
    else if (n % 12 === 10) part = "| Case | Before | After |\n| --- | --- | --- |\n| Open | 1840 ms | 420 ms |\n| Switch | 960 ms | 310 ms |";
    else part = `${Array.from({ length: 3 + Math.floor(rand() * 3) }, () => sentence(rand)).join(" ")} See [trace ${n}](https://example.com/trace/${n}) and \`useIssueTimeline\`.`;
    parts.push(part);
    size += part.length + 2;
  }
  return parts.join("\n\n");
}

/** A pasted server log: many short lines. */
function log(chars: number): string {
  const lines: string[] = [];
  let size = 0;
  for (let i = 0; size < chars; i++) {
    const line = `2026-11-25T08:${String(Math.floor(i / 60) % 60).padStart(2, "0")}:${String(i % 60).padStart(2, "0")}.${String(i % 1000).padStart(3, "0")}Z INFO request_id=${(i * 2654435761 >>> 0).toString(16)} method=GET path=/api/issues/${i % 977}/timeline status=200 duration_ms=${(i * 37) % 900}`;
    lines.push(line);
    size += line.length + 1;
  }
  return lines.join("\n");
}

/** A machine-sized export pasted as is: one line of JSON. */
function json(chars: number): string {
  const rows: unknown[] = [];
  let size = 2;
  for (let i = 0; size < chars; i++) {
    const row = { id: i, identifier: `MUL-${i}`, status: ["todo", "in_progress", "done"][i % 3], title: SENTENCES[i % SENTENCES.length], labels: ["perf", `area-${i % 9}`], estimate: (i * 7) % 13 };
    rows.push(row);
    size += JSON.stringify(row).length + 1;
  }
  return JSON.stringify(rows);
}

interface Dataset { name: string; huge: string | null }

const DATASETS: Dataset[] = [
  { name: "control", huge: null },
  { name: "prose-30k", huge: prose(30_000) },
  { name: "prose-100k", huge: prose(100_000) },
  { name: "log-300k", huge: log(300_000) },
  { name: "json-100k", huge: json(100_000) },
  { name: "json-2mb", huge: json(2_000_000) },
];

/** Time a trivial round trip into the page; null when the main thread did not answer. */
async function probe(page: Page, timeoutMs: number): Promise<number | null> {
  const start = performance.now();
  const answered = await Promise.race([
    page.evaluate(() => new Promise<boolean>((done) => requestAnimationFrame(() => done(true)))).catch(() => false),
    new Promise<boolean>((done) => setTimeout(() => done(false), timeoutMs)),
  ]);
  return answered ? performance.now() - start : null;
}

test("measure an issue with one huge comment against the local API", async ({ browser }) => {
  test.skip(process.env.PERF_SCENARIO !== "huge-comment", "Requires an isolated local API and production build");
  test.setTimeout(1_800_000);
  expect(process.env.MULTICA_DEV_PROFILE).toMatch(/^dev-/);
  expect(process.env.DATABASE_URL).toBeTruthy();
  const api = await createTestApi();
  const db = new pg.Client(process.env.DATABASE_URL);
  await db.connect();
  const samples: Record<string, unknown>[] = [];
  const repeats = Number(process.env.PERF_REPEATS ?? 3);
  const selected = DATASETS.filter((d) => !process.env.PERF_DATASET || process.env.PERF_DATASET.split(",").includes(d.name));
  try {
    const workspace = (await api.getWorkspaces())[0]!;
    const userId = (await db.query('SELECT id FROM "user" WHERE email=$1', [api.getEmail()])).rows[0].id;
    for (const dataset of selected) {
      const title = `Huge comment benchmark ${dataset.name}`;
      const issue = await api.createIssue(title, { description: "Local performance comparison." });
      // The huge body opens the discussion, so it is on screen when the issue
      // opens, followed by ordinary replies. It is the target of both entries
      // and has to mount for a sample to finish.
      const bodies = [dataset.huge ?? shortComment(0), ...Array.from({ length: 6 }, (_, i) => shortComment(i + 1))];
      const ids: string[] = [];
      for (const [i, body] of bodies.entries()) {
        ids.push((await db.query(`
          INSERT INTO comment (workspace_id,issue_id,author_type,author_id,content,created_at)
          VALUES ($1,$2,'member',$3,$4,now()-interval '1 day'+$5::int*interval '1 second') RETURNING id
        `, [workspace.id, issue.id, userId, body, i])).rows[0].id);
      }
      const targetId = ids[0]!;
      await api.seedCommentNotification(issue.id, targetId, title);

      for (const entry of ["direct", "inbox"] as const) {
        if (process.env.PERF_ENTRY && entry !== process.env.PERF_ENTRY) continue;
        // One warmup, then fresh-browser-context samples. A frozen sample ends
        // the dataset: the remaining ones would only wait out the same timeout.
        for (let round = -1; round < repeats; round++) {
          const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
          let frozen = false;
          try {
            await context.addInitScript((token) => {
              localStorage.setItem("multica_token", token!);
              localStorage.setItem("multica:chat:isOpen", "false");
              const state = { longTasks: [] as number[] };
              (window as unknown as { __hugePerf: typeof state }).__hugePerf = state;
              new PerformanceObserver((entries) => {
                for (const item of entries.getEntries()) state.longTasks.push(item.duration);
              }).observe({ type: "longtask", buffered: false });
            }, api.getToken());
            const page = await context.newPage();
            const errors: string[] = [];
            page.on("pageerror", (error) => errors.push(error.message));
            const session = await context.newCDPSession(page);
            await session.send("Performance.enable");
            if (entry === "inbox") {
              await page.goto(`/${workspace.slug}/inbox`);
              await expect(page.getByText(title, { exact: true }).first()).toBeVisible();
              await page.evaluate(() => { (window as unknown as { __hugePerf: { longTasks: number[] } }).__hugePerf.longTasks.length = 0; });
            }
            const before = await session.send("Performance.getMetrics");
            const start = performance.now();
            if (entry === "direct") await page.goto(`/${workspace.slug}/issues/${issue.id}`, { waitUntil: "commit" });
            else await page.getByText(title, { exact: true }).first().click();
            let readyMs: number | null = null;
            // Playwright's own timeout runs inside the page and never fires
            // while its main thread is blocked, so the deadline lives here.
            const visible = page.waitForFunction((targetId) => {
                const container = document.querySelector<HTMLElement>('[data-tab-scroll-root^="main:"]');
                const target = document.querySelector(`[data-comment-content="${targetId}"]`);
                if (!container || !target || !target.textContent) return false;
                const c = container.getBoundingClientRect();
                const r = target.getBoundingClientRect();
                return r.bottom > c.top && r.top < c.bottom;
              }, targetId, { timeout: 0, polling: 50 }).then(() => true, () => false);
            let timer: ReturnType<typeof setTimeout> | undefined;
            const deadline = new Promise<false>((done) => { timer = setTimeout(() => done(false), READY_TIMEOUT_MS); });
            const shown = await Promise.race([visible, deadline]);
            clearTimeout(timer);
            if (shown) {
              await page.evaluate(async () => {
                for (let i = 0; i < 2; i++) await new Promise((done) => requestAnimationFrame(done));
              });
              readyMs = performance.now() - start;
            } else {
              // Timed out. If the page still answers, the target never showed
              // up and the scenario is broken; only a silent page is frozen.
              if (await probe(page, 5_000) !== null) {
                const state = await page.evaluate((targetId) => {
                  const target = document.querySelector(`[data-comment-content="${targetId}"]`);
                  return {
                    url: location.href,
                    roots: [...document.querySelectorAll("[data-tab-scroll-root]")].map((e) => e.getAttribute("data-tab-scroll-root")),
                    target: target ? { text: target.textContent?.length, rect: target.getBoundingClientRect().toJSON() } : null,
                  };
                }, targetId);
                throw new Error(`${dataset.name}/${entry}: target comment not visible but page responsive: ${JSON.stringify(state)}`);
              }
              frozen = true;
            }
            // Once the body is on screen, is the page usable? A frame request
            // that takes seconds means the user is still staring at a stuck tab.
            const probeMs = frozen ? null : await probe(page, 10_000);
            const record: Record<string, unknown> = {
              dataset: dataset.name, entry, round,
              huge_chars: dataset.huge?.length ?? 0,
              huge_sha256: createHash("sha256").update(dataset.huge ?? "").digest("hex").slice(0, 16),
              ready_ms: readyMs, frozen, probe_ms: probeMs,
              system_load_1m: loadavg()[0],
            };
            if (!frozen) {
              await page.waitForTimeout(500);
              const after = await session.send("Performance.getMetrics");
              const metric = (r: typeof after, name: string) => r.metrics.find((m) => m.name === name)?.value ?? 0;
              const dom = await page.evaluate((targetId) => ({
                nodes: document.getElementsByTagName("*").length,
                target_height_px: Math.round(document.querySelector(`[data-comment-content="${targetId}"]`)?.getBoundingClientRect().height ?? 0),
                longTasks: [...(window as unknown as { __hugePerf: { longTasks: number[] } }).__hugePerf.longTasks],
              }), targetId);
              await session.send("HeapProfiler.collectGarbage");
              const memory = await session.send("Performance.getMetrics");
              Object.assign(record, {
                task_ms: (metric(after, "TaskDuration") - metric(before, "TaskDuration")) * 1000,
                long_task_count: dom.longTasks.length,
                long_task_max_ms: Math.max(0, ...dom.longTasks),
                long_task_total_ms: dom.longTasks.reduce((sum, v) => sum + v, 0),
                heap_mb: metric(memory, "JSHeapUsedSize") / 1e6,
                dom_nodes: dom.nodes, target_height_px: dom.target_height_px,
              });
              expect(errors).toEqual([]);
            }
            if (round >= 0 || frozen) {
              samples.push(record);
              console.log(JSON.stringify(record));
            }
          } finally { await context.close(); }
          if (frozen) break;
        }
      }
    }
    if (process.env.PERF_REPORT_PATH) writeFileSync(process.env.PERF_REPORT_PATH, JSON.stringify({
      scenario: "huge-comment", ref: process.env.PERF_REF, browser: browser.version(), cpu_throttle: 1,
      viewport: "1440x900", ready_timeout_ms: READY_TIMEOUT_MS, samples, status: "ok",
    }, null, 2));
  } finally {
    await api.cleanup();
    await db.end();
  }
});
