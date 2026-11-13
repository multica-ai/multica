/** Real API/database and production UI; opt in with PERF_SCENARIO=issue-history. */
import { test, expect } from "@playwright/test";
import { createHash } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";
import { loadavg } from "node:os";
import pg from "pg";
import { createTestApi } from "../helpers";

test("measure issue history entry against the local API", async ({ browser }) => {
  test.skip(process.env.PERF_SCENARIO !== "issue-history", "Requires an isolated local API and production build");
  test.setTimeout(600_000);
  expect(process.env.MULTICA_DEV_PROFILE).toMatch(/^dev-/);
  expect(process.env.DATABASE_URL).toBeTruthy();
  const api = await createTestApi();
  const db = new pg.Client(process.env.DATABASE_URL);
  await db.connect();
  const samples: Record<string, unknown>[] = [];
  const repeats = Number(process.env.PERF_REPEATS ?? 5);
  const datasets = [120, 716].map((count) => ({
    name: `generated-${count}-replies`,
    bodies: ["Performance discussion", ...Array.from({ length: count }, (_, i) =>
      `Reply ${i}\n\n${"A measured discussion about implementation, validation and tradeoffs. ".repeat(3 + i % 8)}\n\n` +
      (i % 5 === 0 ? "```ts\nconst result = await query({ issueId, view: 'timeline' });\nreturn result.map(task => task.status);\n```" : "- Verify the result\n- Preserve the reading position"))],
    runs: false,
  }));
  if (process.env.PERF_ISSUE_SOURCE) {
    const comments = JSON.parse(readFileSync(process.env.PERF_ISSUE_SOURCE, "utf8")) as { content?: string }[];
    datasets.unshift({ name: "local-discussion-text", bodies: comments.map((c) => c.content ?? "").filter(Boolean), runs: false });
  }
  datasets.push({ name: "20-runs-64KiB-output", bodies: ["Execution history benchmark"], runs: true });
  let runtimeId = "";
  let agentId = "";
  try {
    const workspace = (await api.getWorkspaces())[0]!;
    const userId = (await db.query('SELECT id FROM "user" WHERE email=$1', [api.getEmail()])).rows[0].id;
    for (const dataset of datasets.filter((item) => !process.env.PERF_DATASET || process.env.PERF_DATASET.split(",").includes(item.name))) {
      const title = `History benchmark ${dataset.name}`;
      const issue = await api.createIssue(title, { description: "Local performance comparison." });
      let rootId = "";
      let targetId = "";
      for (const [i, body] of dataset.bodies.entries()) {
        const comment = (await db.query(`
          INSERT INTO comment (workspace_id,issue_id,author_type,author_id,content,parent_id,created_at)
          VALUES ($1,$2,'member',$3,$4,$5,now()-interval '1 day'+$6::int*interval '1 second') RETURNING id
        `, [workspace.id, issue.id, userId, body, rootId || null, i])).rows[0].id;
        if (!rootId) rootId = comment;
        if (i === Math.floor((dataset.bodies.length - 1) * 0.8)) targetId = comment;
      }
      await api.seedCommentNotification(issue.id, targetId, title);
      if (dataset.runs) {
        runtimeId = (await db.query(`INSERT INTO agent_runtime
          (workspace_id,name,runtime_mode,provider,status,device_info,metadata,owner_id)
          VALUES ($1,'Benchmark runtime','cloud','test','offline','Test','{}',$2) RETURNING id`, [workspace.id, userId])).rows[0].id;
        agentId = (await db.query(`INSERT INTO agent
          (workspace_id,name,description,instructions,runtime_mode,runtime_config,runtime_id,visibility,permission_mode,max_concurrent_tasks,owner_id)
          VALUES ($1,'Benchmark agent','','','cloud','{}',$2,'private','private',1,$3) RETURNING id`, [workspace.id, runtimeId, userId])).rows[0].id;
        for (let i = 0; i < 20; i++) await db.query(`INSERT INTO agent_task_queue
          (agent_id,runtime_id,issue_id,status,result,handoff_note,started_at,completed_at)
          VALUES ($1,$2,$3,'completed',$4,$5,now()-interval '2 minutes',now()-interval '1 minute')`,
        [agentId, runtimeId, issue.id, JSON.stringify({ comment: "Inline summary", output: "provider output ".repeat(4096) }), "Handoff context. ".repeat(100)]);
      }

      for (const entry of ["direct", "inbox"] as const) {
        if (process.env.PERF_ENTRY && entry !== process.env.PERF_ENTRY) continue;
        // One warmup primes server/OS caches; each measured sample gets a fresh browser context.
        for (let round = -1; round < repeats; round++) {
          const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
          try {
            await context.addInitScript((token) => {
              localStorage.setItem("multica_token", token!);
              localStorage.setItem("multica:chat:isOpen", "false");
              const state = { longTasks: [] as number[] };
              (window as unknown as { __historyPerf: typeof state }).__historyPerf = state;
              new PerformanceObserver((entries) => {
                for (const item of entries.getEntries()) state.longTasks.push(item.duration);
              }).observe({ type: "longtask", buffered: false });
            }, api.getToken());
            const page = await context.newPage();
            const errors: string[] = [];
            page.on("pageerror", (error) => errors.push(error.message));
            const responses: Promise<{ path: string; bytes: number }>[] = [];
            page.on("response", (response) => {
              const path = new URL(response.url()).pathname;
              if (path.includes(`/issues/${issue.id}/`) && /\/(timeline|task-runs|deliverables)$/.test(path)) {
                responses.push(response.body().then((body) => ({ path, bytes: body.length })));
              }
            });
            const session = await context.newCDPSession(page);
            await session.send("Performance.enable");
            if (entry === "inbox") {
              await page.goto(`/${workspace.slug}/inbox`);
              await expect(page.getByText(title, { exact: true }).first()).toBeVisible();
              await page.evaluate(() => { (window as unknown as { __historyPerf: { longTasks: number[] } }).__historyPerf.longTasks.length = 0; });
            }
            const before = await session.send("Performance.getMetrics");
            const start = performance.now();
            if (entry === "direct") await page.goto(`/${workspace.slug}/issues/${issue.id}`);
            else await page.getByText(title, { exact: true }).first().click();
            await page.waitForFunction(({ targetId, rootId, entry }) => {
              const container = document.querySelector<HTMLElement>('[data-tab-scroll-root^="main:"]');
              const target = entry === "inbox"
                ? document.getElementById(`comment-${targetId}`)
                : container?.querySelector(`[data-comment-content="${rootId}"]`);
              if (!container || !target || target.getAttribute("data-virtual-thread-mounted") === "false") return false;
              const c = container.getBoundingClientRect();
              const r = target.getBoundingClientRect();
              return r.bottom > c.top && r.top < c.bottom;
            }, { targetId, rootId, entry }, { timeout: 45_000 });
            await page.evaluate(async () => {
              for (let i = 0; i < 4; i++) await new Promise((done) => requestAnimationFrame(done));
            });
            const readyMs = performance.now() - start;
            await page.waitForTimeout(350);
            const after = await session.send("Performance.getMetrics");
            const metric = (record: typeof after, name: string) => record.metrics.find((m) => m.name === name)?.value ?? 0;
            const taskMs = (metric(after, "TaskDuration") - metric(before, "TaskDuration")) * 1000;
            // Snapshot interaction work before the instrumentation's forced GC.
            const dom = await page.evaluate(() => ({
              nodes: document.getElementsByTagName("*").length,
              bodies: document.querySelectorAll('[data-comment-content]:not([data-comment-content^="description:"])').length,
              longTasks: [...(window as unknown as { __historyPerf: { longTasks: number[] } }).__historyPerf.longTasks],
            }));
            await session.send("HeapProfiler.collectGarbage");
            const memory = await session.send("Performance.getMetrics");
            const network = await Promise.all(responses);
            expect(errors).toEqual([]);
            if (round >= 0) {
              const record = {
                dataset: dataset.name, entry, round, ready_ms: readyMs, task_ms: taskMs,
                system_load_1m: loadavg()[0],
                heap_mb: metric(memory, "JSHeapUsedSize") / 1e6,
                dom_nodes: dom.nodes, mounted_bodies: dom.bodies,
                long_task_count: dom.longTasks.length,
                long_task_max_ms: Math.max(0, ...dom.longTasks),
                long_task_total_ms: dom.longTasks.reduce((sum, value) => sum + value, 0),
                task_response_bytes: network.filter((r) => r.path.endsWith("/task-runs")).reduce((sum, r) => sum + r.bytes, 0),
                dataset_comments: dataset.bodies.length,
                dataset_utf8_bytes: Buffer.byteLength(dataset.bodies.join("\n")),
                dataset_sha256: createHash("sha256").update(JSON.stringify(dataset.bodies)).digest("hex"),
              };
              samples.push(record);
              console.log(JSON.stringify(record));
            }
          } finally { await context.close(); }
        }
      }
    }
    if (process.env.PERF_REPORT_PATH) writeFileSync(process.env.PERF_REPORT_PATH, JSON.stringify({
      ref: process.env.PERF_REF, browser: browser.version(), cpu_throttle: 1, viewport: "1440x900",
      samples, status: "ok",
    }, null, 2));
  } finally {
    await api.cleanup();
    if (agentId) await db.query("DELETE FROM agent_task_queue WHERE agent_id=$1", [agentId]);
    if (agentId) await db.query("DELETE FROM agent WHERE id=$1", [agentId]);
    if (runtimeId) await db.query("DELETE FROM agent_runtime WHERE id=$1", [runtimeId]);
    await db.end();
  }
});
