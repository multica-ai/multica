/**
 * Inbox heap growth (MUL-9634): click through the Work panel's twenty Inbox
 * rows, round and round, as `desktop-memory` does, and read the JS heap after
 * a forced GC at checkpoints. Retained memory that grows with the number of
 * switches is a leak; a heap that levels off is caches filling.
 *
 * Runs on the web build (default) or the Desktop renderer (PERF_PLATFORM=
 * desktop). PERF_HEAP_CHECKPOINTS lists switch counts (default
 * 0,40,80,160,320); PERF_HEAP_SNAPSHOT_DIR also writes a heap snapshot at each
 * checkpoint for offline diffing.
 */
import { expect, test } from "@playwright/test";
import { createWriteStream, mkdirSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import type { CDPSession } from "@playwright/test";
import { settle } from "./lib/frame-metrics";
import { ensureDataset, perfApi, resetInbox, type Manifest } from "./lib/core-dataset";
import { inboxStorage, openInbox, switchTo } from "./lib/core-steps";
import { perfTarget, PLATFORM, type Target } from "./lib/perf-target";

const enabled = process.env.PERF_SCENARIO === "inbox-heap";
const CHECKPOINTS = (process.env.PERF_HEAP_CHECKPOINTS ?? "0,40,80,160,320").split(",").map(Number);
const SNAPSHOT_DIR = process.env.PERF_HEAP_SNAPSHOT_DIR;
const SWITCH_DWELL_MS = 1000;
const VIEWPORT = { width: 1440, height: 900 };

let manifest: Manifest;
let token: string;
let target: Target;

test.beforeAll(async ({ browser }) => {
  test.skip(!enabled, "set PERF_SCENARIO=inbox-heap");
  expect(process.env.MULTICA_DEV_PROFILE, "run through `make env-exec` against an isolated environment").toMatch(/^dev-/);
  test.setTimeout(900_000);
  const api = await perfApi();
  token = api.getToken()!;
  manifest = await ensureDataset(api);
  target = await perfTarget(browser, token);
});

async function gcHeap(cdp: CDPSession) {
  for (let i = 0; i < 3; i++) await cdp.send("HeapProfiler.collectGarbage");
  const metrics = (await cdp.send("Performance.getMetrics")).metrics;
  const get = (name: string) => metrics.find((m) => m.name === name)?.value ?? 0;
  const dom = await cdp.send("Memory.getDOMCounters");
  return {
    js_heap_mb: Math.round(get("JSHeapUsedSize") / 1e5) / 10,
    nodes: dom.nodes,
    documents: dom.documents,
    listeners: dom.jsEventListeners,
  };
}

async function snapshot(cdp: CDPSession, path: string) {
  mkdirSync(dirname(path), { recursive: true });
  const out = createWriteStream(path);
  const onChunk = ({ chunk }: { chunk: string }) => out.write(chunk);
  cdp.on("HeapProfiler.addHeapSnapshotChunk", onChunk);
  await cdp.send("HeapProfiler.takeHeapSnapshot", { reportProgress: false });
  cdp.off("HeapProfiler.addHeapSnapshotChunk", onChunk);
  await new Promise<void>((done) => out.end(done));
}

test("heap while switching Inbox rows", async () => {
  test.setTimeout(3_600_000);
  await resetInbox(manifest);
  const sample = await target.newSample({ token, viewport: VIEWPORT, cpuThrottle: 1, storage: inboxStorage(manifest.workspace.slug) });
  const { page, cdp } = sample;
  try {
    await cdp.send("HeapProfiler.enable");
    const rows = await openInbox(sample, manifest, "20");
    const order = manifest.inbox.slice(0, 20).map((n) => manifest.issues[n.key]!);
    const points: Record<string, unknown>[] = [];
    const last = Math.max(...CHECKPOINTS);
    for (let i = 0; i <= last; i++) {
      if (CHECKPOINTS.includes(i)) {
        await settle(page, 1500);
        const point = { switches: i, ...(await gcHeap(cdp)) };
        points.push(point);
        console.log(JSON.stringify(point));
        if (SNAPSHOT_DIR) await snapshot(cdp, join(SNAPSHOT_DIR, `${PLATFORM}-${i}.heapsnapshot`));
      }
      if (i === last) break;
      const issue = order[i % order.length]!;
      await switchTo(page, rows, issue, "in-view");
      await settle(page, SWITCH_DWELL_MS);
    }
    if (process.env.PERF_REPORT_PATH) {
      mkdirSync(dirname(process.env.PERF_REPORT_PATH), { recursive: true });
      writeFileSync(process.env.PERF_REPORT_PATH, JSON.stringify({ platform: PLATFORM, ref: process.env.PERF_REF ?? null, points }, null, 2));
    }
  } finally {
    await sample.close();
  }
});
