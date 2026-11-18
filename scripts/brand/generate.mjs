#!/usr/bin/env node
// Rebuilds every brand asset from packages/ui/lib/multica-brand.ts. Prerequisites
// and the list of outputs: scripts/brand/README.md.
//
//   node scripts/brand/generate.mjs
import { chromium } from "@playwright/test";
import { spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { INK, PAPER, loadBrand, svgs, sync } from "./sync.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const root = resolve(here, "../..");
const brand = await loadBrand(root);
const { WORDMARK, WORDMARK_STROKE, STAR_GRAPHIC_STROKE, starPath } = brand;
const { tile, wordmark } = svgs(brand);

function run(cmd, args) {
  const r = spawnSync(cmd, args, { stdio: "inherit" });
  if (r.error?.code === "ENOENT") throw new Error(`${cmd} not found; see scripts/brand/README.md`);
  if (r.status !== 0) throw new Error(`${cmd} ${args.join(" ")} exited ${r.status}`);
}

const out = mkdtempSync(join(tmpdir(), "multica-brand-"));
const repo = (rel) => join(root, rel);
const pngs = [];

const updated = await sync(root);
if (updated.length) console.log(`updated ${updated.join(", ")}`);

const inter = createRequire(repo("apps/desktop/package.json")).resolve("@fontsource-variable/inter/files/inter-latin-wght-normal.woff2");
const fonts = `@font-face{font-family:"Inter";font-weight:100 900;src:url(data:font/woff2;base64,${readFileSync(inter).toString("base64")}) format("woff2")}`;

// Full Chrome for Testing, which rasterises on the GPU like the committed bitmaps.
// The default headless shell (and --disable-gpu) rasterises in software and
// antialiases differently, so every bitmap would change.
const browser = await chromium.launch({ channel: "chromium" });
try {
  const page = await browser.newPage({ deviceScaleFactor: 1 });
  async function png(file, [w, h], body, { alpha = true } = {}) {
    mkdirSync(dirname(file), { recursive: true });
    await page.setViewportSize({ width: w, height: h });
    await page.setContent(`<html><head><style>${fonts}*{margin:0}html,body{width:${w}px;height:${h}px;overflow:hidden;background:transparent}svg{display:block}</style></head><body>${body}</body></html>`);
    await page.evaluate(() => document.fonts.ready);
    await page.screenshot({ path: file, omitBackground: alpha });
    if (file.startsWith(root)) pngs.push(file);
  }
  const at = (svg, px, off) => `<div style="position:absolute;left:${off}px;top:${off}px;width:${px}px;height:${px}px">${svg}</div>`;

  // Desktop and PWA tiles: [tile px, margin] per canvas size.
  const FLAT = { 16: [16, 0], 24: [24, 0], 32: [30, 1], 48: [46, 1], 64: [52, 6], 128: [104, 12], 256: [208, 24], 512: [412, 50] };
  for (const [s, [px, off]] of Object.entries(FLAT)) {
    await png(repo(`apps/desktop/build/icons/${s}x${s}.png`), [+s, +s], at(tile(px), px, off));
    if (+s <= 256) await png(join(out, `ico-${s}.png`), [+s, +s], at(tile(px), px, off));
  }
  await png(repo("apps/desktop/build/icon.png"), [1024, 1024], at(tile(824), 824, 100));
  await png(repo("apps/web/public/icons/icon-192.png"), [192, 192], at(tile(156), 156, 18));
  await png(repo("apps/web/public/icons/icon-512.png"), [512, 512], at(tile(412), 412, 50));

  // iOS, Android and Expo mask these themselves, so they are full-bleed and opaque.
  await png(repo("apps/web/public/icons/apple-touch-icon.png"), [180, 180], tile(180, { bleed: true }), { alpha: false });
  await png(repo("apps/web/public/icons/icon-maskable-512.png"), [512, 512], tile(512, { bleed: true }), { alpha: false });
  await png(repo("apps/mobile/assets/icon.png"), [1024, 1024], tile(1024, { bleed: true }), { alpha: false });

  // macOS: the tile on Apple's 1024 grid (body 100..924) with the standard soft shadow.
  const macOS = (S) => {
    const k = S / 1024, px = Math.round(824 * k), off = Math.round(100 * k);
    return `<div style="position:absolute;left:${off}px;top:${off}px;width:${px}px;height:${px}px;filter:drop-shadow(0 ${(22 * k).toFixed(2)}px ${(30 * k).toFixed(2)}px rgba(0,0,0,.32))">${tile(px)}</div>`;
  };
  await png(repo("apps/desktop/resources/icon.png"), [1024, 1024], macOS(1024));
  const iconset = join(out, "icon.iconset");
  for (const [name, S] of [["16x16", 16], ["16x16@2x", 32], ["32x32", 32], ["32x32@2x", 64], ["128x128", 128], ["128x128@2x", 256], ["256x256", 256], ["256x256@2x", 512], ["512x512", 512], ["512x512@2x", 1024]]) {
    await png(join(iconset, `icon_${name}.png`), [S, S], macOS(S));
  }

  // OG card: the wordmark and headline beside a team of thin stars, one per
  // teammate. The lit one is the newest to join.
  const TEAM = { cols: 3, rows: 4, size: 96, gap: 36, right: 76, lit: 7 };
  const pitch = TEAM.size + TEAM.gap;
  const teamLeft = 1200 - TEAM.right - (TEAM.cols * pitch - TEAM.gap);
  const teamTop = (630 - (TEAM.rows * pitch - TEAM.gap)) / 2;
  const thinStar = starPath(STAR_GRAPHIC_STROKE);
  const team = Array.from({ length: TEAM.cols * TEAM.rows }, (_, i) =>
    `<svg viewBox="0 0 100 100" style="position:absolute;width:${TEAM.size}px;height:${TEAM.size}px;left:${teamLeft + (i % TEAM.cols) * pitch}px;top:${teamTop + Math.floor(i / TEAM.cols) * pitch}px"><path fill="${i === TEAM.lit ? PAPER : "#2c2c30"}" d="${thinStar}"/></svg>`).join("");
  await png(repo("apps/web/app/opengraph-image.png"), [1200, 630], `<div style="position:relative;width:1200px;height:630px;background:${INK};overflow:hidden;font-family:Inter">
  ${team}
  <div style="position:absolute;left:64px;top:72px;height:52px">${wordmark(PAPER, ` height="52"`)}</div>
  <div style="position:absolute;left:64px;bottom:70px;color:#fff;font:600 60px/1.12 Inter;letter-spacing:-.02em;white-space:nowrap">People and agents,<br>working as one team.</div></div>`, { alpha: false });
  await page.close();

  // README hero: the write-on from MulticaWordmark (timings in multica-wordmark.tsx,
  // keyframes in packages/ui/styles/base.css). Transform animations do not advance
  // under CDP virtual time, so every animation is paused and seeked per frame. The
  // seeked transform reaches the compositor a frame or two later; capturing sooner
  // leaves a few star pixels from the previous frame, differently on every run.
  const W = 720, H = 256, FPS = 30, TOTAL_MS = 2500, START_MS = 100, LETTER_MS = 150, STAR_AFTER_MS = 6 * LETTER_MS + 420;
  const css = `@keyframes write { from { stroke-dashoffset: 1; } }
@keyframes star-in { from { transform: scale(0) rotate(-135deg); } }
.wl { stroke-dasharray: 1 1; animation: write .5s cubic-bezier(.65,0,.35,1) both; }
.ws { transform-box: fill-box; transform-origin: 50% 50%; animation: star-in .9s cubic-bezier(.34,1.56,.64,1) backwards; }`;
  const writeOn = (c) => `<svg viewBox="${WORDMARK.viewBox}" width="643" style="display:block"><g fill="none" stroke="${c}" stroke-width="${WORDMARK_STROKE}">${WORDMARK.letters.map((l, i) => `<path class="wl" pathLength="1" style="animation-delay:${START_MS + i * LETTER_MS}ms" d="${l.d}"/>${l.star ? `<path class="ws" style="animation-delay:${START_MS + STAR_AFTER_MS}ms" fill="${c}" stroke="none" d="${l.star}"/>` : ""}`).join("")}</g></svg>`;
  for (const [name, bg, fg] of [["light", PAPER, INK], ["dark", "#0d1117", PAPER]]) {
    const dir = join(out, `gif-${name}`);
    mkdirSync(dir);
    const gif = await browser.newPage({ viewport: { width: W, height: H }, deviceScaleFactor: 1 });
    await gif.setContent(`<html><head><style>*{margin:0}html,body{width:${W}px;height:${H}px;background:${bg};overflow:hidden}body{display:grid;place-items:center}${css}</style></head><body>${writeOn(fg)}</body></html>`);
    await gif.evaluate(() => document.getAnimations().forEach((a) => a.pause()));
    for (let k = 0; k < Math.round((TOTAL_MS / 1000) * FPS); k++) {
      await gif.evaluate(async (t) => {
        document.getAnimations().forEach((a) => { a.currentTime = t; });
        for (let i = 0; i < 2; i++) await new Promise(requestAnimationFrame);
      }, (k * 1000) / FPS);
      await gif.screenshot({ path: join(dir, `f${String(k).padStart(4, "0")}.png`) });
    }
    await gif.close();
  }
} finally {
  await browser.close();
}

run("python3", [join(here, "pack.py"), out, root]);
if (process.platform === "darwin") run("iconutil", ["-c", "icns", join(out, "icon.iconset"), "-o", repo("apps/desktop/build/icon.icns")]);
else console.warn("iconutil is macOS-only: apps/desktop/build/icon.icns was not rebuilt");
run("oxipng", ["-o", "max", "--strip", "safe", "-q", ...pngs]);
rmSync(out, { recursive: true, force: true });
console.log("done");
