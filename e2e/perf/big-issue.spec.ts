/**
 * Opening a big issue directly (MUL-9539 scenario 1): time until the title and
 * first comments are on screen, stalls while the page loads, while scrolling
 * the whole thread as its images load, and while toggling the sidebar.
 * Real API and database seeded by scripts/perf-seed.mjs; opt in with
 * PERF_SCENARIO=big-issue and PERF_MANIFEST=<manifest>.
 */
import { test, expect, type Page } from "@playwright/test";
import {
  inputAt, inputCount, loadManifest, median, newMeasuredContext, now, percentile, repeats, round,
  signIn, stallsBetween, throttle, waitFor, waitForQuiet, writeReport, type ManifestIssue,
} from "./lib/probe";

// Roughly 3,000 px/s for at most 15 s: a fast read through the thread.
const SCROLL_SECONDS = 15;
const WHEEL_PX = 100;
const WHEEL_INTERVAL_MS = 25;
const SIDEBAR_TOGGLES = 6;

test("big issue: open, scroll and toggle the sidebar", async ({ browser }) => {
  test.skip(process.env.PERF_SCENARIO !== "big-issue", "Requires a seeded local API and a production build");
  test.setTimeout(1_800_000);
  const manifest = loadManifest();
  const kinds = (process.env.PERF_ISSUE_KINDS ?? "p99,big,xl").split(",");
  const targets = kinds.flatMap((kind) => (manifest.issues[kind as keyof typeof manifest.issues] ?? []).slice(0, 1));
  expect(targets.length).toBeGreaterThan(0);
  const auth = await signIn(browser, manifest.user.email);
  const samples: Record<string, unknown>[] = [];

  for (const issue of targets) {
    // One warmup primes the server and OS caches; every sample is a fresh context.
    for (let round_ = -1; round_ < repeats(); round_++) {
      const context = await newMeasuredContext(browser, auth);
      try {
        const page = await context.newPage();
        const errors: string[] = [];
        page.on("pageerror", (error) => errors.push(error.message));
        await throttle(page);
        const sample = await measure(page, manifest.workspace.slug, issue);
        expect(errors).toEqual([]);
        if (round_ >= 0) {
          samples.push({ issue: issue.kind, round: round_, ...sample });
          console.log(JSON.stringify({ issue: issue.kind, round: round_, open_visible_ms: sample.open.visible_ms, open_settled_ms: sample.open.settled_ms }));
        }
      } finally {
        await context.close();
      }
    }
  }

  const summary = Object.fromEntries(targets.map((issue) => {
    const of = samples.filter((s) => s.issue === issue.kind) as unknown as Sample[];
    const toggles = of.flatMap((s) => s.sidebar.toggles.map((t) => t.done_ms));
    return [issue.kind, {
      shape: issue.shape,
      open_visible_ms: round(median(of.map((s) => s.open.visible_ms))),
      open_settled_ms: round(median(of.map((s) => s.open.settled_ms))),
      open_longest_stall_ms: round(median(of.map((s) => s.open.stalls.longest_ms))),
      open_long_frame_ms: round(median(of.map((s) => s.open.stalls.long_frame_ms))),
      scroll_longest_stall_ms: round(median(of.map((s) => s.scroll.stalls.longest_ms))),
      scroll_long_frame_ms: round(median(of.map((s) => s.scroll.stalls.long_frame_ms))),
      scroll_stalls: median(of.map((s) => s.scroll.stalls.stalls)),
      sidebar_toggle_ms_median: round(median(toggles)),
      sidebar_toggle_ms_p95: round(percentile(toggles, 95)),
      sidebar_longest_stall_ms: round(median(of.map((s) => s.sidebar.stalls.longest_ms))),
      samples: of.length,
    }];
  }));
  writeReport("big-issue", browser, manifest, samples, summary);
});

interface Sample {
  open: { visible_ms: number; settled_ms: number; stalls: Awaited<ReturnType<typeof stallsBetween>> };
  scroll: { duration_ms: number; distance_px: number; images_fetched: number; image_mb: number; stalls: Awaited<ReturnType<typeof stallsBetween>> };
  sidebar: { toggles: { state: string; done_ms: number }[]; stalls: Awaited<ReturnType<typeof stallsBetween>> };
  dom_nodes: number;
}

async function measure(page: Page, slug: string, issue: ManifestIssue): Promise<Sample> {
  // Open: page time is measured from navigation start, so app boot is included.
  await page.goto(`/${slug}/issues/${issue.id}`, { waitUntil: "commit" });
  const visible = await waitFor(page, "issueReady", { title: issue.title, expectComments: issue.shape.comments > 0 });
  const settled = await waitForQuiet(page, 0);
  const open = { visible_ms: round(visible), settled_ms: round(Math.max(visible, settled)), stalls: await stallsBetween(page, 0, Math.max(visible, settled) + 1) };

  // Scroll down with the mouse wheel at a steady pace while images load. Wheel
  // input scrolls on the compositor the way a trackpad does, so a busy main
  // thread shows up as long frames, not as a scroll that never moves.
  const scroller = page.locator('[data-tab-scroll-root^="main:"]');
  const box = (await scroller.boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  const imageBytes: Promise<number>[] = [];
  const onResponse = (response: import("@playwright/test").Response) => {
    if (response.request().resourceType() === "image" && response.status() === 200) imageBytes.push(response.body().then((b) => b.length, () => 0));
  };
  page.on("response", onResponse);
  const scrollStart = await now(page);
  const wallStart = Date.now();
  let lastTop = -1;
  for (let i = 1; Date.now() - wallStart < SCROLL_SECONDS * 1000; i++) {
    await page.mouse.wheel(0, WHEEL_PX);
    await page.waitForTimeout(WHEEL_INTERVAL_MS);
    if (i % 20 !== 0) continue;
    // Stop at the bottom: a shorter thread ends before the time does.
    const top = await scroller.evaluate((el) => el.scrollTop);
    if (top === lastTop) break;
    lastTop = top;
  }
  lastTop = await scroller.evaluate((el) => el.scrollTop);
  const scrollEnd = await now(page);
  page.off("response", onResponse);
  const bytes = await Promise.all(imageBytes);
  const scrollResult = {
    duration_ms: round(scrollEnd - scrollStart),
    distance_px: Math.round(lastTop),
    images_fetched: bytes.length,
    image_mb: round(bytes.reduce((sum, b) => sum + b, 0) / 1e6),
    stalls: await stallsBetween(page, scrollStart, scrollEnd),
  };
  const scroll = { end: scrollEnd };
  await waitForQuiet(page, scroll.end, 500);

  // Toggle the navigation sidebar; each toggle is done when the content column
  // has stopped changing width.
  const sidebarStart = await now(page);
  const toggles: { state: string; done_ms: number }[] = [];
  for (let i = 0; i < SIDEBAR_TOGGLES; i++) {
    const before = await inputCount(page);
    const done = page.evaluate(() => new Promise<{ end: number; state: string }>((resolve) => {
      const root = document.querySelector<HTMLElement>('[data-tab-scroll-root^="main:"]') ?? document.querySelector<HTMLElement>("[data-tab-scroll-root]")!;
      const startWidth = root.getBoundingClientRect().width;
      let last = startWidth;
      let stable = 0;
      let changed = false;
      const t0 = performance.now();
      const tick = () => {
        const width = root.getBoundingClientRect().width;
        if (width !== startWidth) changed = true;
        stable = width === last ? stable + 1 : 0;
        last = width;
        if ((changed && stable >= 3) || performance.now() - t0 > 5_000) {
          const state = document.querySelector('[data-slot="sidebar"][data-state]')?.getAttribute("data-state") ?? (changed ? "changed" : "unchanged");
          resolve({ end: performance.now(), state });
        } else requestAnimationFrame(tick);
      };
      requestAnimationFrame(tick);
    }));
    await page.keyboard.press("ControlOrMeta+b");
    const result = await done;
    const input = await inputAt(page, before);
    toggles.push({ state: result.state, done_ms: round(result.end - input) });
    await page.waitForTimeout(300);
  }
  const sidebarEnd = await now(page);
  const sidebar = { toggles, stalls: await stallsBetween(page, sidebarStart, sidebarEnd) };

  return { open, scroll: scrollResult, sidebar, dom_nodes: await page.evaluate(() => document.getElementsByTagName("*").length) };
}
