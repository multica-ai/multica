#!/usr/bin/env node
/**
 * Verify UI motion in the running web app: sample element boxes every
 * animation frame, check the samples, and record a 2x GIF of the same
 * interaction. Static screenshots cannot show a jump, an overshoot or a
 * motion that ignores reduced motion, and jsdom has no layout to animate.
 *
 *   make up C=api,web ARGS="--ephemeral"
 *   make env-exec ARGS="-- node scripts/motion/capture.mjs scripts/motion/inbox-tabs.mjs --out ../motion"
 *
 * The rules this checks, and when a UI PR needs it, are in
 * packages/ui/docs/motion.md.
 *
 * A scenario is an ES module whose default export receives:
 *
 *   open(path, options)   a signed-in page at /<workspace><path>, in a fresh
 *                         browser context at deviceScaleFactor 2. Options:
 *                         reducedMotion, locale, theme ("light" | "dark"),
 *                         viewport, storage (extra localStorage entries).
 *   sample(page, label, targets, action, options)
 *                         records the box of every target (name → CSS
 *                         selector, first match) on each animation frame from
 *                         before `action()` until nothing has moved for
 *                         `quietMs` (default 400). Returns the frames.
 *   record(page, name, clip, action, options)
 *                         writes <out>/<name>.gif of `clip` (a selector or
 *                         { x, y, width, height }) while `action()` runs.
 *   delay(page, urlGlob, ms, { times })
 *                         holds matching requests, to reproduce a slow load.
 *   check(label, result)  adds a line to the report; results come from
 *                         scripts/motion/checks.mjs, passed in as `checks`.
 *   api(method, path, body), slug, out, args, log
 *
 * Output in --out: report.json (every check and every sampled frame) and the
 * GIFs. The exit code is 1 when a check failed.
 *
 * Options:
 *   --out DIR    output directory (default ../motion, outside the checkout)
 *   --url URL    app origin (default this environment's web port)
 *   Any other argument after the scenario path goes to the scenario as `args`.
 *
 * Limits:
 * - Sample and record in separate passes. Each screenshot delays animation
 *   frames, so frame counts from a recorded pass are wrong.
 * - Headless Chromium runs animation frames at 120Hz: a 200ms tween spans
 *   about 24 sampled frames.
 * - A 2x screenshot of a panel-sized region takes about 40ms, so GIFs come
 *   out at roughly 20–25fps, fewer on a busy machine. The GIF keeps the real
 *   timing whatever the rate, and the log prints the rate reached. Judge
 *   smoothness from the samples, not the GIF. A shot without a clip is 1x.
 * - Web only. For Desktop, drive the app with scripts/desktop-cdp.mjs.
 */
import { createRequire } from "node:module";
import { execFileSync, spawnSync } from "node:child_process";
import { mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";

import * as checks from "./checks.mjs";

const repoRoot = execFileSync("git", ["rev-parse", "--show-toplevel"]).toString().trim();
const { chromium } = createRequire(join(repoRoot, "package.json"))("@playwright/test");

function fail(message) {
  console.error(`[motion] ${message}`);
  process.exit(1);
}
const log = (message) => console.log(`[motion] ${message}`);

const argv = process.argv.slice(2);
const options = { out: resolve(repoRoot, "../motion"), url: "", scenario: "", args: [] };
for (let i = 0; i < argv.length; i++) {
  const arg = argv[i];
  if (arg === "--out") options.out = resolve(argv[++i]);
  else if (arg === "--url") options.url = argv[++i];
  else if (options.scenario) options.args.push(arg);
  else if (arg.startsWith("--")) fail(`unknown option ${arg}`);
  else options.scenario = resolve(arg);
}
if (!options.scenario) fail("usage: capture.mjs [--out DIR] [--url URL] <scenario.mjs> [scenario args...]");

const env = process.env;
if (!env.PORT || !/^dev-/.test(env.MULTICA_DEV_PROFILE ?? "")) {
  fail('run through `make env-exec ARGS="-- node scripts/motion/capture.mjs ..."`.');
}
const base = (options.url || `http://localhost:${env.FRONTEND_PORT}`).replace(/\/+$/, "");
const profilesHome = env.MULTICA_DEV_PROFILES_HOME || join(homedir(), ".multica", "profiles");
const profile = JSON.parse(readFileSync(join(profilesHome, env.MULTICA_DEV_PROFILE, "config.json"), "utf8"));
const slug = JSON.parse(execFileSync("bash", [join(repoRoot, "scripts", "dev-env.sh"), "status", "--json"]).toString()).workspace_slug;

async function api(method, path, body) {
  const response = await fetch(`http://localhost:${env.PORT}${path}`, {
    method,
    headers: { Authorization: `Bearer ${profile.token}`, "X-Workspace-ID": profile.workspace_id, "Content-Type": "application/json" },
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!response.ok) throw new Error(`${method} ${path}: ${response.status} ${await response.text()}`);
  return response.status === 204 ? null : response.json();
}

const SCALE = 2;
const browser = await chromium.launch();
const contexts = [];

async function open(path, { reducedMotion = false, locale = "en-US", theme = "light", viewport = { width: 1440, height: 900 }, storage = {} } = {}) {
  const context = await browser.newContext({
    viewport,
    locale,
    deviceScaleFactor: SCALE,
    colorScheme: theme,
    reducedMotion: reducedMotion ? "reduce" : "no-preference",
  });
  contexts.push(context);
  await context.addInitScript(
    ([token, theme, storage]) => {
      localStorage.setItem("multica_token", token);
      localStorage.setItem("theme", theme);
      localStorage.setItem("multica:chat:isOpen", "false");
      for (const [key, value] of Object.entries(storage)) localStorage.setItem(key, value);
    },
    [profile.token, theme, storage],
  );
  const page = await context.newPage();
  await page.goto(`${base}/${slug}${path}`, { waitUntil: "domcontentloaded" });
  // The Next.js dev indicator sits over the bottom-left corner of every frame.
  await page.addStyleTag({ content: "nextjs-portal { display: none !important; }" });
  return page;
}

const samples = [];
async function sample(page, label, targets, action, { quietMs = 400, timeoutMs = 8000 } = {}) {
  await page.evaluate((targets) => {
    const state = { frames: [], lastChange: performance.now(), actionDone: Infinity, stopped: false, previous: "" };
    window.__motionSampler = state;
    const t0 = performance.now();
    const frame = (now) => {
      if (state.stopped) return;
      const rects = {};
      for (const [name, selector] of Object.entries(targets)) {
        const el = document.querySelector(selector);
        const r = el?.getBoundingClientRect();
        rects[name] = r ? { x: r.x, y: r.y, w: r.width, h: r.height } : null;
      }
      const key = JSON.stringify(rects);
      if (key !== state.previous) state.lastChange = now;
      state.previous = key;
      state.frames.push({ t: Math.round((now - t0) * 10) / 10, rects });
      requestAnimationFrame(frame);
    };
    requestAnimationFrame(frame);
  }, targets);
  await page.waitForFunction(() => window.__motionSampler.frames.length >= 2);
  await action();
  await page.evaluate(() => { window.__motionSampler.actionDone = performance.now(); });
  try {
    await page.waitForFunction(
      (quietMs) => {
        const state = window.__motionSampler;
        return performance.now() - Math.max(state.lastChange, state.actionDone) >= quietMs;
      },
      quietMs,
      { timeout: timeoutMs, polling: 50 },
    );
  } catch {
    log(`${label}: still moving after ${timeoutMs}ms; the report holds what was sampled`);
  }
  const frames = await page.evaluate(() => {
    window.__motionSampler.stopped = true;
    return window.__motionSampler.frames;
  });
  samples.push({ label, targets, frames });
  return frames;
}

const results = [];
function check(label, result) {
  results.push({ label, ...result });
  console.log(`${result.ok ? "PASS" : "FAIL"}  ${label} — ${result.detail}`);
}

async function delay(page, urlGlob, ms, { times = 1 } = {}) {
  await page.route(urlGlob, async (route) => {
    await new Promise((r) => setTimeout(r, ms));
    await route.continue();
  }, { times });
}

/**
 * Screenshots of one region, taken back to back through CDP and joined with
 * ffmpeg. The clip's `scale` is output pixels per CSS pixel, so the GIF is 2x;
 * a CDP screencast is 1x only, and scaling the page with CSS `zoom` doubles
 * every getBoundingClientRect() a sampling pass reads.
 */
async function record(page, name, clip, action, { fps = 35, leadMs = 250, tailMs = 700, padding = 0 } = {}) {
  let rect = clip;
  if (typeof clip === "string") {
    const box = await page.locator(clip).first().boundingBox();
    if (!box) throw new Error(`record ${name}: ${clip} is not on the page`);
    rect = box;
  }
  rect = {
    x: Math.max(0, Math.floor(rect.x - padding)),
    y: Math.max(0, Math.floor(rect.y - padding)),
    width: Math.ceil(rect.width + padding * 2),
    height: Math.ceil(rect.height + padding * 2),
  };
  const cdp = await page.context().newCDPSession(page);
  const shots = [];
  let done = false;
  const interval = 1000 / fps;
  const capture = (async () => {
    while (!done) {
      const started = Date.now();
      // JPEG encodes faster than PNG at 2x; the GIF palette absorbs its artifacts.
      const { data } = await cdp.send("Page.captureScreenshot", {
        format: "jpeg",
        quality: 92,
        clip: { ...rect, scale: SCALE },
        optimizeForSpeed: true,
      });
      shots.push({ t: started, data });
      const wait = interval - (Date.now() - started);
      if (wait > 0) await new Promise((r) => setTimeout(r, wait));
    }
  })();
  try {
    await new Promise((r) => setTimeout(r, leadMs));
    await action();
    await new Promise((r) => setTimeout(r, tailMs));
  } finally {
    done = true;
    await capture.catch(() => {});
    await cdp.detach().catch(() => {});
  }

  const dir = join(options.out, `${name}.frames`);
  rmSync(dir, { recursive: true, force: true });
  mkdirSync(dir, { recursive: true });
  const lines = ["ffconcat version 1.0"];
  shots.forEach((shot, i) => {
    const file = `${String(i).padStart(5, "0")}.jpg`;
    writeFileSync(join(dir, file), Buffer.from(shot.data, "base64"));
    const next = shots[i + 1]?.t ?? shot.t + 1000;
    lines.push(`file '${file}'`, `duration ${((next - shot.t) / 1000).toFixed(3)}`);
  });
  // The concat demuxer drops the last entry's duration unless it is listed again.
  lines.push(`file '${String(shots.length - 1).padStart(5, "0")}.jpg'`);
  writeFileSync(join(dir, "frames.ffconcat"), `${lines.join("\n")}\n`);

  const gif = join(options.out, `${name}.gif`);
  const ffmpeg = [
    "-y", "-loglevel", "error", "-f", "concat", "-safe", "0", "-i", join(dir, "frames.ffconcat"),
    "-vf", `fps=${fps},split[a][b];[a]palettegen=stats_mode=diff[p];[b][p]paletteuse=dither=bayer:bayer_scale=5:diff_mode=rectangle`,
    "-loop", "0", gif,
  ];
  const encoded = spawnSync("ffmpeg", ffmpeg, { stdio: "inherit" });
  const seconds = (shots.at(-1).t - shots[0].t) / 1000;
  const rate = Math.round((shots.length - 1) / seconds);
  if (encoded.status !== 0) {
    log(`${name}: ffmpeg failed; ${shots.length} frames kept in ${dir}`);
    return null;
  }
  rmSync(dir, { recursive: true, force: true });
  log(`${name}: ${shots.length} frames at ~${rate}fps, ${rect.width * SCALE}×${rect.height * SCALE}px → ${gif}`);
  return gif;
}

mkdirSync(options.out, { recursive: true });
const commit = execFileSync("git", ["rev-parse", "--short", "HEAD"], { cwd: repoRoot }).toString().trim();
const dirty = execFileSync("git", ["status", "--porcelain", "--untracked-files=no"], { cwd: repoRoot }).toString().trim() !== "";
log(`commit ${commit}${dirty ? " + uncommitted changes" : ""}, ${base}, out ${options.out}`);

const scenario = await import(pathToFileURL(options.scenario).href);
try {
  await scenario.default({ open, sample, record, delay, check, checks, api, slug, out: options.out, args: options.args, log });
} finally {
  for (const context of contexts) await context.close().catch(() => {});
  await browser.close();
  writeFileSync(
    join(options.out, "report.json"),
    `${JSON.stringify({ commit, dirty, scenario: options.scenario.replace(`${repoRoot}/`, ""), args: options.args, checks: results, samples }, null, 1)}\n`,
  );
}

const failed = results.filter((r) => !r.ok).length;
log(`${results.length - failed}/${results.length} checks passed; report → ${join(options.out, "report.json")}`);
process.exit(failed > 0 ? 1 : 0);
