#!/usr/bin/env node
/**
 * Time a hover-then-click navigation in the web app under added network
 * latency: from the click to the target content on screen (MUL-9796).
 *
 *   make env-exec ARGS="-- node scripts/web-nav-timing.mjs --label dev"    # next dev
 *   make web-prod ARGS="-- node scripts/web-nav-timing.mjs --label prod"   # production build
 *
 * Measure navigation on the production build (scripts/web-prod.mjs). `next
 * dev` does not prefetch routes on hover and serves every module unbundled,
 * so its numbers are slower and point at the wrong bottleneck. Run it on dev
 * only to compare the two.
 *
 * The default scenario opens a doc. It creates a doc of 24 paragraphs with a
 * comment, opens it once so the collaboration service saves its state, then,
 * from the docs home page, hovers the doc's link, clicks it, and records
 * every animation frame until the page settles. --from, --link and --until
 * time any other link instead:
 *
 *   ... web-nav-timing.mjs --from /dev/issues --link "Fix login" --until "h1"
 *
 * Every sample runs in a fresh browser context: empty HTTP cache, nothing
 * prefetched, the first navigation of a session. The start page loads at
 * full speed, and the latency applies from the hover on. One warmup sample
 * runs first and is not counted; on dev it absorbs route compilation.
 *
 * Times start at the click event's own timestamp and end in the first frame
 * where the target is laid out; sampling is per animation frame. Requests
 * are listed relative to the click (negative: started during the hover).
 * WebSockets are not delayed by the latency emulation.
 *
 * Options:
 *   --url URL           app origin (default $WEB_PROD_URL, else this environment's web port)
 *   --latency MS        round-trip latency added to each request (default 250)
 *   --hover MS          hover before the click (default 150)
 *   --settle MS         wait on the start page before hovering, so idle prefetches run (default 3000)
 *   --repeats N         measured samples (default 5)
 *   --warmup N          uncounted samples first (default 1)
 *   --from PATH         start page (doc scenario: /<workspace>/docs)
 *   --link NAME         text in the accessible name of the link to hover and click (the last match)
 *   --until SELECTOR    CSS selector of the content that counts as on screen; elements
 *                       already on the start page do not count
 *   --label NAME        printed with the results and used for the doc's title
 *   --json FILE         write every sample
 *   --screenshot FILE   the last sample, after it settled
 */
import { createRequire } from "node:module";
import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { join, resolve } from "node:path";

const repoRoot = execFileSync("git", ["rev-parse", "--show-toplevel"]).toString().trim();
const { chromium } = createRequire(join(repoRoot, "package.json"))("@playwright/test");

const args = process.argv.slice(2);
const flag = (name, fallback) => {
  const at = args.indexOf(`--${name}`);
  return at >= 0 && args[at + 1] !== undefined ? args[at + 1] : fallback;
};
function fail(message) {
  console.error(`[web-nav-timing] ${message}`);
  process.exit(1);
}
const env = process.env;
if (!env.PORT || !/^dev-/.test(env.MULTICA_DEV_PROFILE ?? "")) {
  fail('run through `make env-exec ARGS="-- node scripts/web-nav-timing.mjs ..."` or `make web-prod`.');
}

const label = flag("label", "run");
const latency = Number(flag("latency", 250));
const hoverMs = Number(flag("hover", 150));
const settleMs = Number(flag("settle", 3000));
const repeats = Number(flag("repeats", 5));
const warmup = Number(flag("warmup", 1));
const base = (flag("url", env.WEB_PROD_URL || `http://localhost:${env.FRONTEND_PORT}`)).replace(/\/+$/, "");
const jsonOut = flag("json");
const screenshot = flag("screenshot");
const custom = { from: flag("from"), link: flag("link"), until: flag("until") };
if (Object.values(custom).some(Boolean) && !Object.values(custom).every(Boolean)) {
  fail("--from, --link and --until go together");
}

const profile = JSON.parse(readFileSync(join(homedir(), ".multica/profiles", env.MULTICA_DEV_PROFILE, "config.json"), "utf8"));
const apiBase = `http://localhost:${env.PORT}`;
async function api(method, path, body) {
  const response = await fetch(apiBase + path, {
    method,
    headers: { Authorization: `Bearer ${profile.token}`, "X-Workspace-ID": profile.workspace_id, "Content-Type": "application/json" },
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!response.ok) throw new Error(`${method} ${path}: ${response.status} ${await response.text()}`);
  return response.json();
}
const slug = JSON.parse(execFileSync("bash", [join(repoRoot, "scripts", "dev-env.sh"), "status", "--json"]).toString()).workspace_slug;

const browser = await chromium.launch();
async function newContext() {
  // The selectors below match English labels ("Live", "Connecting").
  const context = await browser.newContext({ viewport: { width: 1400, height: 900 }, locale: "en-US" });
  await context.addInitScript((token) => localStorage.setItem("multica_token", token), profile.token);
  return context;
}

// Body text the doc scenario waits for, in either renderer: the read-only
// copy shown first or the live editor.
const DOC_MARKER = "Paragraph 1.";
async function seedDoc() {
  const paragraphs = Array.from({ length: 24 }, (_, i) =>
    i === 3
      ? "This paragraph holds the anchor phrase that a discussion points at, so its highlight shows."
      : `Paragraph ${i + 1}. A line of ordinary prose so the doc is long enough to scroll and lay out.`,
  );
  const content = [
    "# Navigation timing", ...paragraphs.slice(0, 8),
    "## Second part", ...paragraphs.slice(8, 16),
    "## Third part", ...paragraphs.slice(16),
  ].join("\n\n");
  const title = `Navigation timing ${label} ${Date.now().toString(36)}`;
  const doc = await api("POST", "/api/docs", { title, content });
  await api("POST", `/api/docs/${doc.id}/comments`, {
    content: "Worth a look",
    anchor_quote: "anchor phrase",
    anchor_prefix: "This paragraph holds the ",
    anchor_suffix: " that a discussion points at",
  });
  // A doc opened live once has a saved collaboration state, which is what
  // later opens start from. Without collab it saves through the API instead.
  const ticket = await api("POST", `/api/docs/${doc.id}/collab-ticket`, {});
  if (ticket.enabled) {
    const context = await newContext();
    const page = await context.newPage();
    await page.goto(`${base}/${slug}/docs/${doc.id}`);
    await page.getByText("Live", { exact: true }).waitFor({ timeout: 30_000 });
    await context.close();
    for (let i = 0; i < 40; i++) {
      if ((await api("POST", `/api/docs/${doc.id}/collab-ticket`, { state: true })).state) break;
      await new Promise((done) => setTimeout(done, 250));
    }
  }
  return { from: `/${slug}/docs`, link: title, live: Boolean(ticket.enabled) };
}

let scenario;

// Runs in the page. States are sampled every frame and recorded on change,
// until __nav.stop() or the safety cap.
function installRecorder({ kind, until, marker }) {
  const w = window;
  const visible = (el) => !!el && el.getClientRects().length > 0 && getComputedStyle(el).visibility !== "hidden";
  w.__nav = { frames: [], click: null };
  document.addEventListener("click", (event) => { w.__nav.click ??= event.timeStamp; }, { capture: true });
  w.__nav.start = (capSeconds) => {
    const t0 = performance.now();
    let last = "";
    const sample = () => {
      const state = {};
      if (kind === "doc") {
        const body = [...document.querySelectorAll(".ProseMirror, .rich-text-editor.readonly")]
          .find((el) => visible(el) && el.textContent.includes(marker));
        state.content = !!body;
        state.skeleton = !!document.querySelector("div[aria-busy].max-w-4xl");
        state.readonly = !!body && body.matches(".readonly");
        state.editor = !!body && body.matches(".ProseMirror");
        state.highlight = !!globalThis.CSS?.highlights?.get("doc-comment")?.size;
        state.connecting = [...document.querySelectorAll('[role="status"]')].some((s) => /Connecting/.test(s.textContent ?? ""));
        state.live = [...document.querySelectorAll('[role="status"]')].some((s) => /\bLive\b/.test(s.textContent ?? ""));
      } else {
        state.content = [...document.querySelectorAll(until)].some((el) => !before.has(el) && visible(el));
      }
      state.route = location.pathname !== w.__nav.from;
      const key = Object.entries(state).filter(([, on]) => on).map(([name]) => name).join("+");
      if (key !== last) {
        w.__nav.frames.push({ at: performance.now(), state: key });
        last = key;
      }
      if (!w.__nav.stopped && performance.now() - t0 < capSeconds * 1000) requestAnimationFrame(sample);
    };
    w.__nav.from = location.pathname;
    w.__nav.stop = () => { w.__nav.stopped = true; };
    // The start page's own matches are not the destination's content.
    const before = new Set(until ? document.querySelectorAll(until) : []);
    sample();
  };
}

async function sample(index, keep) {
  const context = await newContext();
  await context.addInitScript(installRecorder, {
    kind: scenario.kind,
    until: scenario.until,
    marker: DOC_MARKER,
  });
  const page = await context.newPage();
  const requests = [];
  page.on("request", (request) => requests.push(request));
  await page.goto(base + scenario.from);
  const link = page.getByRole("link", { name: scenario.link }).last();
  await link.waitFor({ timeout: 30_000 }).catch(() => {
    throw new Error(`no link named "${scenario.link}" on ${scenario.from}`);
  });
  await page.waitForTimeout(settleMs);
  requests.length = 0;

  const cdp = await context.newCDPSession(page);
  await cdp.send("Network.enable");
  await cdp.send("Network.emulateNetworkConditions", { offline: false, latency, downloadThroughput: -1, uploadThroughput: -1 });
  const hoverAt = Date.now();
  await link.hover();
  await page.waitForTimeout(hoverMs);
  await page.evaluate(() => window.__nav.start(60));
  await link.click();
  const until = Date.now() + 15_000;
  while (!(await page.evaluate(() => window.__nav.frames.some((f) => f.state.split("+").includes("content"))))) {
    if (Date.now() > until) break;
    await page.waitForTimeout(100);
  }
  await page.waitForTimeout(2000);
  const nav = await page.evaluate(() => {
    window.__nav.stop();
    const { frames, click } = window.__nav;
    return { frames, click, origin: performance.timeOrigin };
  });
  if (keep && screenshot) await page.screenshot({ path: resolve(screenshot), animations: "disabled" });
  await context.close();

  if (nav.click === null) throw new Error("the click never reached the page");
  const clickEpoch = nav.origin + nav.click;
  const timeline = nav.frames.map((f) => ({ t: Math.round(f.at - nav.click), state: f.state }));
  const firstOf = (name) => timeline.find((f) => f.state.split("+").includes(name))?.t ?? null;
  const states = [...new Set(timeline.flatMap((f) => f.state.split("+")).filter(Boolean))];
  const result = {
    index,
    content_ms: firstOf("content"),
    first: Object.fromEntries(states.map((name) => [name, firstOf(name)])),
    timeline,
    requests: requests
      .filter((r) => r.timing().startTime >= hoverAt - 50)
      .map((r) => {
        const timing = r.timing();
        const url = new URL(r.url());
        return {
          start: Math.round(timing.startTime - clickEpoch),
          end: timing.responseEnd >= 0 ? Math.round(timing.startTime + timing.responseEnd - clickEpoch) : null,
          method: r.method(),
          type: r.resourceType(),
          path: url.pathname + (url.search.length > 1 ? url.search.slice(0, 60) : ""),
        };
      })
      .filter((r) => r.start < 5000)
      .sort((a, b) => a.start - b.start),
  };
  if (result.content_ms === null) throw new Error(`sample ${index}: the target never appeared on screen`);
  return result;
}

const samples = [];
let failure = null;
try {
  scenario = custom.from ? { ...custom, kind: "custom" } : { ...(await seedDoc()), kind: "doc" };
  for (let i = 0; i < warmup + repeats; i++) {
    const result = await sample(i, i === warmup + repeats - 1);
    if (i >= warmup) samples.push(result);
    console.log(`[web-nav-timing] ${i < warmup ? "warmup" : `sample ${i - warmup + 1}/${repeats}`}: content at ${result.content_ms}ms`);
  }
} catch (error) {
  failure = error;
} finally {
  await browser.close();
}
if (failure) fail(failure.message);

const median = (values) => {
  const sorted = values.filter((v) => v !== null).sort((a, b) => a - b);
  if (sorted.length === 0) return null;
  const mid = Math.floor(sorted.length / 2);
  return sorted.length % 2 ? sorted[mid] : Math.round((sorted[mid - 1] + sorted[mid]) / 2);
};
const states = [...new Set(samples.flatMap((s) => Object.keys(s.first)))];
const last = samples.at(-1);
const pad = (value, width) => String(value ?? "-").padStart(width);

console.log(`\n${label}: ${base}${scenario.from} -> "${scenario.link}"`);
console.log(`latency ${latency}ms, hover ${hoverMs}ms, ${repeats} samples (fresh context each)\n`);
console.log("last sample, requests (ms from click):");
for (const r of last.requests) {
  if (!/_rsc=|\/api\/|\.js$|\.css$|^\/collab|^\/ws/.test(r.path)) continue;
  console.log(`  ${pad(r.start, 6)} -> ${pad(r.end, 6)}  ${r.method.padEnd(4)} ${r.type.padEnd(8)} ${r.path}`);
}
console.log("\nlast sample, frames (ms from click):");
for (const { t, state } of last.timeline) console.log(`  ${pad(t, 6)}  ${state || "(nothing)"}`);
console.log("\nfirst frame with each state (ms from click):");
console.log(`  ${"state".padEnd(12)}${"median".padStart(8)}  samples`);
for (const name of ["content", ...states.filter((s) => s !== "content")]) {
  const values = samples.map((s) => s.first[name] ?? null);
  console.log(`  ${name.padEnd(12)}${pad(median(values), 8)}  ${values.map((v) => v ?? "-").join(", ")}`);
}

if (jsonOut) {
  writeFileSync(resolve(jsonOut), JSON.stringify({
    label, base, scenario, latency_ms: latency, hover_ms: hoverMs, settle_ms: settleMs,
    content_ms_median: median(samples.map((s) => s.content_ms)), samples,
  }, null, 2));
}
