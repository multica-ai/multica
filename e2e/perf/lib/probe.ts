/**
 * Shared measurement for the large-workspace scenarios (MUL-9539).
 *
 * Two numbers matter: how long until the content a user asked for is on
 * screen, and how long the main thread was stuck while getting there. Both
 * are read inside the page — the input's own event timestamp, the frame that
 * first shows the content, and Long Animation Frame entries — so Playwright's
 * round trips never land in a measurement.
 */
import type { Browser, BrowserContext, Page } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { cpus, loadavg } from "node:os";
import { dirname, resolve } from "node:path";
import pg from "pg";

export interface ManifestIssue {
  id: string;
  identifier: string;
  title: string;
  kind: string;
  shape: { comments: number; images: number; runs: number; children: number };
  first_comment_id: string | null;
}

export interface ManifestInboxRow {
  inbox_item_id: string;
  issue_id: string;
  title: string;
  kind: string;
  target_comment_id: string | null;
  comments: number;
}

export interface PerfManifest {
  dataset: string;
  dataset_version: number;
  tier: string;
  seed: number;
  content_sha256: string;
  workspace: { id: string; slug: string };
  user: { id: string; email: string };
  counts: Record<string, number>;
  issues: { big: ManifestIssue[]; p99: ManifestIssue[]; xl: ManifestIssue[] };
  inbox: ManifestInboxRow[];
}

export const VIEWPORT = { width: 1440, height: 900 };
/** A frame at or over this is a stall a user notices. */
export const STALL_MS = 200;
/** Long Animation Frame threshold: frames over this are "blocked" time. */
export const LONG_FRAME_MS = 50;

export function loadManifest(): PerfManifest {
  const path = process.env.PERF_MANIFEST;
  if (!path) throw new Error("PERF_MANIFEST must point at the manifest scripts/perf-seed.mjs wrote");
  return JSON.parse(readFileSync(path, "utf8")) as PerfManifest;
}

export function cpuThrottle(): number {
  return Number(process.env.PERF_CPU_THROTTLE ?? 1);
}

export function repeats(): number {
  return Number(process.env.PERF_REPEATS ?? 5);
}

type StorageState = Awaited<ReturnType<BrowserContext["storageState"]>>;

export function baseURL(): string {
  return process.env.PLAYWRIGHT_BASE_URL ?? "http://localhost:3000";
}

/**
 * Sign in the way the production web app does: the verification-code flow
 * through the app's own origin, which leaves the HttpOnly session cookie that
 * images and API calls then carry. A token in localStorage would put the app in
 * desktop-style token mode, where every image takes a 401 and a retry.
 */
export async function signIn(browser: Browser, email: string): Promise<StorageState> {
  // Like TestApiClient.login: clear earlier codes so repeated runs stay under
  // the per-email send-code rate limit.
  const db = new pg.Client(process.env.DATABASE_URL);
  await db.connect();
  try {
    await db.query("DELETE FROM verification_code WHERE email = $1", [email]);
  } finally {
    await db.end();
  }
  const context = await browser.newContext({ baseURL: baseURL() });
  try {
    const sent = await context.request.post("/auth/send-code", { data: { email } });
    if (!sent.ok()) throw new Error(`send-code failed: ${sent.status()} ${await sent.text()}`);
    const code = process.env.MULTICA_DEV_VERIFICATION_CODE || "888888";
    const verified = await context.request.post("/auth/verify-code", { data: { email, code } });
    if (!verified.ok()) throw new Error(`verify-code failed: ${verified.status()} ${await verified.text()}`);
    const state = await context.storageState();
    if (!state.cookies.some((cookie) => cookie.name === "multica_auth")) throw new Error("sign-in left no multica_auth cookie");
    return state;
  } finally {
    await context.close();
  }
}

/**
 * A fresh signed-in context whose pages record input timestamps and Long
 * Animation Frames from navigation start, and expose the readiness checks the
 * scenarios wait on.
 */
export async function newMeasuredContext(browser: Browser, auth: StorageState): Promise<BrowserContext> {
  const context = await browser.newContext({ viewport: VIEWPORT, storageState: { cookies: auth.cookies, origins: [] } });
  await context.addInitScript(() => {
    localStorage.setItem("multica:chat:isOpen", "false");

    type Frame = { start: number; duration: number; blocking: number; scripts: { src: string; fn: string; invoker: string; ms: number }[] };
    const perf = { frames: [] as Frame[], inputs: [] as number[], supported: "" };
    (window as unknown as { __perf: typeof perf }).__perf = perf;
    const record = (event: Event) => perf.inputs.push(event.timeStamp);
    addEventListener("pointerdown", record, { capture: true });
    addEventListener("keydown", record, { capture: true });

    const types = PerformanceObserver.supportedEntryTypes ?? [];
    if (types.includes("long-animation-frame")) {
      perf.supported = "long-animation-frame";
      new PerformanceObserver((list) => {
        for (const entry of list.getEntries() as unknown as {
          startTime: number; duration: number; blockingDuration: number;
          scripts: { sourceURL: string; sourceFunctionName: string; invoker: string; duration: number }[];
        }[]) {
          perf.frames.push({
            start: entry.startTime,
            duration: entry.duration,
            blocking: entry.blockingDuration,
            scripts: entry.scripts
              .slice()
              .sort((a, b) => b.duration - a.duration)
              .slice(0, 3)
              .map((s) => ({ src: s.sourceURL.split("/").pop() ?? "", fn: s.sourceFunctionName, invoker: s.invoker, ms: s.duration })),
          });
        }
      }).observe({ type: "long-animation-frame", buffered: true });
    } else {
      perf.supported = "longtask";
      new PerformanceObserver((list) => {
        for (const entry of list.getEntries()) {
          perf.frames.push({ start: entry.startTime, duration: entry.duration, blocking: Math.max(0, entry.duration - 50), scripts: [] });
        }
      }).observe({ type: "longtask", buffered: true });
    }

    // Readiness checks. Each answers "is the content the user asked for on
    // screen now?" and is polled once per frame by waitFor().
    const inViewport = (el: Element) => {
      const root = el.closest("[data-tab-scroll-root]") ?? document.documentElement;
      const r = el.getBoundingClientRect();
      const c = root.getBoundingClientRect();
      return r.height > 0 && r.bottom > c.top && r.top < c.bottom;
    };
    const mounted = (el: Element) => el.closest("[data-virtual-thread-row]")?.getAttribute("data-virtual-thread-mounted") !== "false";
    const detailFor = (title: string) => {
      for (const detail of document.querySelectorAll('[data-mc="issue-detail"]')) {
        const heading = detail.querySelector('[data-mc="issue-title"]');
        if (heading && heading.getBoundingClientRect().height > 0 && heading.textContent?.includes(title)) return detail;
      }
      return null;
    };
    const checks = {
      /** The issue's title and, when it has any, its first comments are rendered. */
      issueReady({ title, expectComments }: { title: string; expectComments: boolean }) {
        const detail = detailFor(title);
        if (!detail) return false;
        if (!expectComments) return true;
        for (const body of detail.querySelectorAll('[data-mc="issue-activity"] [data-comment-content]')) {
          if (mounted(body) && body.getBoundingClientRect().height > 0) return true;
        }
        return false;
      },
      /** The issue is open and the comment a notification points at is in view. */
      commentInView({ title, commentId }: { title: string; commentId: string }) {
        const detail = detailFor(title);
        if (!detail) return false;
        const target = detail.querySelector(`[data-comment-content="${commentId}"]`) ?? detail.querySelector(`#comment-${CSS.escape(commentId)}`);
        return !!target && mounted(target) && inViewport(target);
      },
    };
    (window as unknown as { __perfChecks: typeof checks }).__perfChecks = checks;
  });
  return context;
}

export async function throttle(page: Page) {
  const rate = cpuThrottle();
  if (rate <= 1) return;
  const session = await page.context().newCDPSession(page);
  await session.send("Emulation.setCPUThrottlingRate", { rate });
}

type CheckName = "issueReady" | "commentInView";

/**
 * Resolve with the page time of the first frame after `check` passes. Start it
 * before the input that should make the check pass; it polls once per frame.
 */
export function waitFor(page: Page, check: CheckName, arg: Record<string, unknown>, timeoutMs = 45_000): Promise<number> {
  return page.evaluate(
    ({ check, arg, timeoutMs }) =>
      new Promise<number>((done, fail) => {
        const checks = (window as unknown as { __perfChecks: Record<string, (a: unknown) => boolean> }).__perfChecks;
        const deadline = performance.now() + timeoutMs;
        const tick = () => {
          if (checks[check]!(arg)) {
            // The frame after the check is the one that painted the content.
            requestAnimationFrame(() => done(performance.now()));
            return;
          }
          if (performance.now() > deadline) fail(new Error(`${check} timed out: ${JSON.stringify(arg)}`));
          else requestAnimationFrame(tick);
        };
        tick();
      }),
    { check, arg, timeoutMs },
  );
}

export async function now(page: Page): Promise<number> {
  return page.evaluate(() => performance.now());
}

/** Timestamp of the first input recorded at or after index `from`. */
export async function inputAt(page: Page, from: number): Promise<number> {
  return page.evaluate((from) => (window as unknown as { __perf: { inputs: number[] } }).__perf.inputs[from] ?? NaN, from);
}

export async function inputCount(page: Page): Promise<number> {
  return page.evaluate(() => (window as unknown as { __perf: { inputs: number[] } }).__perf.inputs.length);
}

/** Wait until no long frame has started for `quietMs`; returns when the last one ended. */
export async function waitForQuiet(page: Page, since: number, quietMs = 1000, timeoutMs = 30_000): Promise<number> {
  return page.evaluate(
    ({ since, quietMs, timeoutMs }) =>
      new Promise<number>((done) => {
        const perf = (window as unknown as { __perf: { frames: { start: number; duration: number }[] } }).__perf;
        const deadline = performance.now() + timeoutMs;
        const poll = () => {
          const t = performance.now();
          const lastEnd = perf.frames.filter((f) => f.start >= since).reduce((end, f) => Math.max(end, f.start + f.duration), since);
          if (t - lastEnd >= quietMs || t > deadline) done(lastEnd);
          else setTimeout(poll, 50);
        };
        poll();
      }),
    { since, quietMs, timeoutMs },
  );
}

export interface Stalls {
  /** Frames over 200 ms — what a user feels as the page freezing. */
  stalls: number;
  longest_ms: number;
  /** Total time in frames over 50 ms. */
  long_frame_ms: number;
  long_frames: number;
  top_scripts: { script: string; ms: number }[];
}

/** Long frames that started in [start, end). */
export async function stallsBetween(page: Page, start: number, end: number): Promise<Stalls> {
  const frames = await page.evaluate(
    ({ start, end }) =>
      (window as unknown as { __perf: { frames: { start: number; duration: number; scripts: { src: string; fn: string; invoker: string; ms: number }[] }[] } })
        .__perf.frames.filter((f) => f.start >= start && f.start < end),
    { start, end },
  );
  const scripts = new Map<string, number>();
  for (const frame of frames) {
    for (const s of frame.scripts) {
      const key = `${s.fn || s.invoker} (${s.src})`;
      scripts.set(key, (scripts.get(key) ?? 0) + s.ms);
    }
  }
  return {
    stalls: frames.filter((f) => f.duration >= STALL_MS).length,
    longest_ms: round(Math.max(0, ...frames.map((f) => f.duration))),
    long_frame_ms: round(frames.reduce((sum, f) => sum + f.duration, 0)),
    long_frames: frames.length,
    top_scripts: [...scripts].sort((a, b) => b[1] - a[1]).slice(0, 5).map(([script, ms]) => ({ script, ms: round(ms) })),
  };
}

export function round(value: number, digits = 1): number {
  const k = 10 ** digits;
  return Math.round(value * k) / k;
}

export function median(values: number[]): number {
  if (values.length === 0) return NaN;
  const sorted = [...values].sort((a, b) => a - b);
  const mid = sorted.length >> 1;
  return sorted.length % 2 ? sorted[mid]! : (sorted[mid - 1]! + sorted[mid]!) / 2;
}

export function percentile(values: number[], p: number): number {
  if (values.length === 0) return NaN;
  const sorted = [...values].sort((a, b) => a - b);
  return sorted[Math.min(sorted.length - 1, Math.ceil((p / 100) * sorted.length) - 1)]!;
}

function gitRef(): string {
  if (process.env.PERF_REF) return process.env.PERF_REF;
  try {
    return execFileSync("git", ["rev-parse", "--short", "HEAD"]).toString().trim();
  } catch {
    return "unknown";
  }
}

/** Write the scenario's report: environment, dataset identity, samples and summary. */
export function writeReport(scenario: string, browser: Browser, manifest: PerfManifest, samples: unknown[], summary: unknown) {
  const report = {
    scenario,
    ref: gitRef(),
    app_url: baseURL(),
    browser: browser.version(),
    cpu: `${cpus()[0]?.model ?? "unknown"} × ${cpus().length}`,
    cpu_throttle: cpuThrottle(),
    viewport: `${VIEWPORT.width}x${VIEWPORT.height}`,
    system_load_1m: round(loadavg()[0] ?? 0, 2),
    dataset: { name: manifest.dataset, version: manifest.dataset_version, tier: manifest.tier, content_sha256: manifest.content_sha256, counts: manifest.counts },
    summary,
    samples,
  };
  const path = resolve(process.env.PERF_REPORT_PATH ?? `perf-report/${scenario}-${manifest.tier}.json`);
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, JSON.stringify(report, null, 2));
  console.log(`[perf] report → ${path}`);
  console.log(JSON.stringify(summary, null, 2));
}
