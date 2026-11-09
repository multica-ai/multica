#!/usr/bin/env node
// Capture the managed Desktop's real macOS window (title bar and traffic
// lights included), not just the renderer that page.screenshot() sees.
//
//   make up C=desktop ARGS="--ephemeral --cdp"
//   node scripts/desktop-window-capture.mjs window.png
//   node scripts/desktop-window-capture.mjs --compare origin/main shots/ -- apps/desktop packages/views
//
// The window is found by owner PID only: the process listening on this
// environment's CDP port, which `make status` has already proven belongs to
// the environment. The app name ("Multica Canary") and the window title (the
// current page) are shared with the user's own Desktop, so matching on either
// can capture a private window.
//
// --compare checks out BASE's version of the changed files, relaunches the
// managed Desktop, captures before.png, restores the files, relaunches again
// and captures after.png. The worktree is restored on every exit path; the
// backup directory is printed so an interrupted run can still be recovered.

import { execFileSync, spawnSync } from "node:child_process";
import {
  chmodSync,
  copyFileSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  statSync,
  unlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { deflateSync, inflateSync } from "node:zlib";

const REPO_ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");

const USAGE = `Usage:
  node scripts/desktop-window-capture.mjs [options] OUT.png
  node scripts/desktop-window-capture.mjs --compare BASE [options] OUT_DIR [-- pathspec...]

  --title TEXT        only consider windows whose title contains TEXT
                      (still limited to the CDP owner's windows)
  --settle SECONDS    wait before capturing (default: 0, or 5 after a relaunch)
  --prepare CMD       shell command run before each capture, e.g. a
                      Playwright script that navigates to the page to show
  --compare BASE      capture OUT_DIR/before.png with BASE's version of the
                      changed files and OUT_DIR/after.png with the worktree's

Needs a managed Desktop with a debug port: make up C=desktop ARGS="--cdp".
The calling app needs Screen Recording permission.`;

// The menu bar ghost windows Electron registers are layer 0 and 30pt tall.
const MIN_WINDOW_SIZE = 100;

class CaptureError extends Error {}

function log(message) {
  console.error(`[desktop-window] ${message}`);
}

export function parseCli(argv) {
  const options = { out: "", title: "", settle: null, prepare: "", compare: "", pathspecs: [] };
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    const value = () => {
      if (i + 1 >= argv.length) throw new CaptureError(`${arg} needs a value.\n\n${USAGE}`);
      return argv[++i];
    };
    if (arg === "-h" || arg === "--help") options.help = true;
    else if (arg === "--title") options.title = value();
    else if (arg === "--settle") options.settle = Number(value());
    else if (arg === "--prepare") options.prepare = value();
    else if (arg === "--compare") options.compare = value();
    else if (arg === "--") {
      options.pathspecs = argv.slice(i + 1);
      break;
    } else if (arg.startsWith("-")) throw new CaptureError(`unknown option ${arg}\n\n${USAGE}`);
    else if (!options.out) options.out = arg;
    else throw new CaptureError(`unexpected argument ${arg}\n\n${USAGE}`);
  }
  if (options.help) return options;
  if (!options.out) throw new CaptureError(USAGE);
  if (options.settle !== null && !(Number.isFinite(options.settle) && options.settle >= 0)) {
    throw new CaptureError("--settle must be a number of seconds.");
  }
  if (options.pathspecs.length && !options.compare) throw new CaptureError("a pathspec only applies to --compare.");
  return options;
}

// ---------------------------------------------------------------------------
// Finding the window

function run(cmd, args, options = {}) {
  return spawnSync(cmd, args, { cwd: REPO_ROOT, encoding: "utf8", ...options });
}

function managedDesktop() {
  const result = run("bash", ["scripts/dev-env.sh", "status", "--json"]);
  let status;
  try {
    status = JSON.parse(result.stdout);
  } catch {
    throw new CaptureError(`make status did not return JSON. Is there an environment for this checkout?\n${result.stderr.trim()}`);
  }
  const desktop = status.components?.desktop;
  if (desktop?.state !== "running") {
    throw new CaptureError(`the desktop component is ${desktop?.state ?? "missing"}, not running. Start it with make up C=desktop ARGS="--cdp".`);
  }
  if (!status.desktop_cdp_port) {
    throw new CaptureError(`environment ${status.name} has no CDP port, so its window cannot be told apart. Run make up C=desktop ARGS="--cdp".`);
  }
  return { name: status.name, cdpPort: status.desktop_cdp_port };
}

function cdpOwnerPid(port) {
  const result = run("lsof", ["-nP", `-iTCP:${port}`, "-sTCP:LISTEN", "-t"]);
  const pids = [...new Set(result.stdout.split(/\s+/).filter(Boolean))];
  if (pids.length !== 1) {
    throw new CaptureError(`expected one process listening on CDP port ${port}, found ${pids.length ? pids.join(", ") : "none"}.`);
  }
  return Number(pids[0]);
}

// JXA reaches CoreGraphics without compiling a Swift helper. Only the given
// PID's windows leave this script.
const LIST_WINDOWS_JXA = `
ObjC.import("CoreGraphics");
ObjC.bindFunction("CGPreflightScreenCaptureAccess", ["bool", []]);
function run(argv) {
  const pid = Number(argv[0]);
  const all = ObjC.deepUnwrap(ObjC.castRefToObject(
    $.CGWindowListCopyWindowInfo($.kCGWindowListOptionAll, $.kCGNullWindowID))) || [];
  return JSON.stringify({
    screenRecording: $.CGPreflightScreenCaptureAccess(),
    windows: all.filter((w) => w.kCGWindowOwnerPID === pid).map((w) => ({
      id: w.kCGWindowNumber,
      pid: w.kCGWindowOwnerPID,
      layer: w.kCGWindowLayer,
      onscreen: w.kCGWindowIsOnscreen === true,
      alpha: w.kCGWindowAlpha,
      title: w.kCGWindowName || "",
      bounds: w.kCGWindowBounds,
    })),
  });
}`;

function listWindows(pid) {
  const result = run("osascript", ["-l", "JavaScript", "-e", LIST_WINDOWS_JXA, String(pid)]);
  if (result.status !== 0) throw new CaptureError(`could not list windows: ${result.stderr.trim()}`);
  return JSON.parse(result.stdout);
}

// CGWindowList returns windows front to back, so the first match is the
// frontmost window of the app.
export function pickWindow(windows, pid, title = "") {
  const candidates = windows.filter(
    (w) =>
      w.pid === pid &&
      w.layer === 0 &&
      w.onscreen &&
      w.alpha > 0 &&
      w.bounds.Width >= MIN_WINDOW_SIZE &&
      w.bounds.Height >= MIN_WINDOW_SIZE &&
      (!title || w.title.includes(title)),
  );
  return { window: candidates[0] ?? null, others: candidates.slice(1) };
}

const sleep = (ms) => new Promise((done) => setTimeout(done, ms));

async function captureWindow(out, { title, settle }) {
  if (process.platform !== "darwin") throw new CaptureError("window capture needs macOS.");
  const env = managedDesktop();
  const pid = cdpOwnerPid(env.cdpPort);
  if (settle > 0) await sleep(settle * 1000);

  const { screenRecording, windows } = listWindows(pid);
  if (!screenRecording) {
    throw new CaptureError(
      "Screen Recording permission is missing for the app running this command (Terminal, iTerm, Cursor, the Multica daemon, ...). " +
        "Grant it in System Settings > Privacy & Security > Screen & System Audio Recording, then restart that app.",
    );
  }
  const { window, others } = pickWindow(windows, pid, title);
  if (!window) {
    throw new CaptureError(
      `process ${pid} (CDP :${env.cdpPort}) has no visible window${title ? ` titled "${title}"` : ""}. ` +
        "Is it minimised or on another Space?",
    );
  }
  if (others.length) {
    log(`${others.length + 1} windows match; capturing the frontmost. Others: ${others.map((w) => `"${w.title}"`).join(", ")}. Use --title to choose.`);
  }

  const scratch = mkdtempSync(join(tmpdir(), "desktop-window-"));
  try {
    const raw = join(scratch, "raw.png");
    // -l: this window only, -o: no shadow, -x: no shutter sound.
    const shot = run("screencapture", ["-x", "-o", "-l", String(window.id), raw]);
    if (shot.status !== 0 || !existsSync(raw) || statSync(raw).size === 0) {
      throw new CaptureError(`screencapture failed for window ${window.id}: ${(shot.stderr || "no image written").trim()}`);
    }
    mkdirSync(dirname(resolve(out)), { recursive: true });
    const { width, height } = writeFlattenedPng(raw, out);
    log(
      `wrote ${out} (${width}x${height}px) from window ${window.id} "${window.title}", ` +
        `owner pid ${pid} = CDP :${env.cdpPort} listener of ${env.name}`,
    );
  } finally {
    rmSync(scratch, { recursive: true, force: true });
  }
}

// ---------------------------------------------------------------------------
// Flattening: macOS window captures have transparent rounded corners, which
// most viewers show as black. Composite onto white and drop the alpha channel.
// Colour chunks are kept so the pixels mean what they meant on screen.

const PNG_SIGNATURE = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);
const KEPT_CHUNKS = new Set(["iCCP", "sRGB", "gAMA", "cHRM", "pHYs"]);

const CRC_TABLE = Array.from({ length: 256 }, (_, n) => {
  let c = n;
  for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
  return c >>> 0;
});

function crc32(buffer) {
  let c = 0xffffffff;
  for (const byte of buffer) c = CRC_TABLE[(c ^ byte) & 0xff] ^ (c >>> 8);
  return (c ^ 0xffffffff) >>> 0;
}

function chunk(type, data) {
  const head = Buffer.alloc(8);
  head.writeUInt32BE(data.length, 0);
  head.write(type, 4, "latin1");
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(Buffer.concat([head.subarray(4), data])), 0);
  return Buffer.concat([head, data, crc]);
}

function readChunks(png) {
  if (!png.subarray(0, 8).equals(PNG_SIGNATURE)) throw new CaptureError("capture is not a PNG.");
  const chunks = [];
  for (let at = 8; at < png.length; ) {
    const length = png.readUInt32BE(at);
    const type = png.toString("latin1", at + 4, at + 8);
    chunks.push({ type, data: png.subarray(at + 8, at + 8 + length) });
    at += 12 + length;
    if (type === "IEND") break;
  }
  return chunks;
}

function paeth(a, b, c) {
  const p = a + b - c;
  const pa = Math.abs(p - a);
  const pb = Math.abs(p - b);
  const pc = Math.abs(p - c);
  return pa <= pb && pa <= pc ? a : pb <= pc ? b : c;
}

function unfilter(data, width, height, bpp) {
  const stride = width * bpp;
  const pixels = Buffer.alloc(stride * height);
  for (let y = 0; y < height; y++) {
    const filter = data[y * (stride + 1)];
    const src = y * (stride + 1) + 1;
    const row = y * stride;
    for (let x = 0; x < stride; x++) {
      const left = x >= bpp ? pixels[row + x - bpp] : 0;
      const up = y > 0 ? pixels[row - stride + x] : 0;
      const upLeft = y > 0 && x >= bpp ? pixels[row - stride + x - bpp] : 0;
      const raw = data[src + x];
      let value;
      if (filter === 0) value = raw;
      else if (filter === 1) value = raw + left;
      else if (filter === 2) value = raw + up;
      else if (filter === 3) value = raw + ((left + up) >> 1);
      else if (filter === 4) value = raw + paeth(left, up, upLeft);
      else throw new CaptureError(`bad PNG filter ${filter}.`);
      pixels[row + x] = value & 0xff;
    }
  }
  return pixels;
}

// Decodes the 8-bit, non-interlaced RGB(A) PNGs screencapture writes.
export function decodePng(png) {
  const chunks = readChunks(png);
  const header = chunks.find((c) => c.type === "IHDR")?.data;
  if (!header) throw new CaptureError("capture has no PNG header.");
  const width = header.readUInt32BE(0);
  const height = header.readUInt32BE(4);
  const [depth, colorType, , , interlace] = header.subarray(8, 13);
  if (depth !== 8 || (colorType !== 2 && colorType !== 6) || interlace !== 0) {
    throw new CaptureError(`unexpected capture format (depth ${depth}, colour type ${colorType}, interlace ${interlace}).`);
  }
  const channels = colorType === 6 ? 4 : 3;
  const data = inflateSync(Buffer.concat(chunks.filter((c) => c.type === "IDAT").map((c) => c.data)));
  return { width, height, channels, pixels: unfilter(data, width, height, channels), header, chunks };
}

export function flattenPng(png) {
  const { width, height, channels, pixels: rgba, header, chunks } = decodePng(png);
  if (channels === 3) return { png, width, height };

  // Rows filtered with "Up" (2) compress window chrome far better than raw rows.
  const stride = width * 3;
  const out = Buffer.alloc((stride + 1) * height);
  let previous = Buffer.alloc(stride);
  for (let y = 0; y < height; y++) {
    const row = Buffer.alloc(stride);
    for (let x = 0; x < width; x++) {
      const i = (y * width + x) * 4;
      const alpha = rgba[i + 3];
      for (let c = 0; c < 3; c++) {
        row[x * 3 + c] = Math.round((rgba[i + c] * alpha + 255 * (255 - alpha)) / 255);
      }
    }
    const at = y * (stride + 1);
    out[at] = 2;
    for (let x = 0; x < stride; x++) out[at + 1 + x] = (row[x] - previous[x]) & 0xff;
    previous = row;
  }

  const ihdr = Buffer.from(header);
  ihdr[9] = 2;
  const kept = chunks.filter((c) => KEPT_CHUNKS.has(c.type)).map((c) => chunk(c.type, c.data));
  return {
    png: Buffer.concat([PNG_SIGNATURE, chunk("IHDR", ihdr), ...kept, chunk("IDAT", deflateSync(out, { level: 9 })), chunk("IEND", Buffer.alloc(0))]),
    width,
    height,
  };
}

function writeFlattenedPng(raw, out) {
  const { png, width, height } = flattenPng(readFileSync(raw));
  writeFileSync(out, png);
  return { width, height };
}

// ---------------------------------------------------------------------------
// Before/after: swap the changed files to BASE and back, never touching the
// index. A file added since BASE is removed for "before"; a file deleted since
// BASE comes back for it.

function git(cwd, ...args) {
  return execFileSync("git", args, { cwd, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
}

export function changedFiles(repo, base, pathspecs = []) {
  const output = git(repo, "diff", "--name-status", "--no-renames", "-z", base, "--", ...pathspecs);
  const fields = output.split("\0").filter(Boolean);
  const files = [];
  for (let i = 0; i < fields.length; i += 2) files.push({ status: fields[i], path: fields[i + 1] });
  return files;
}

export function swapToBase(repo, base, files, backupDir) {
  const backups = files.map(({ status, path }) => {
    const file = join(repo, path);
    if (status === "D") return { path, existed: false };
    const backup = join(backupDir, path);
    mkdirSync(dirname(backup), { recursive: true });
    copyFileSync(file, backup);
    return { path, existed: true, backup, mode: statSync(file).mode };
  });
  writeFileSync(join(backupDir, "files.json"), JSON.stringify(backups, null, 2));
  const restore = () => {
    for (const entry of backups) {
      const file = join(repo, entry.path);
      if (entry.existed) {
        mkdirSync(dirname(file), { recursive: true });
        copyFileSync(entry.backup, file);
        chmodSync(file, entry.mode);
      } else {
        rmSync(file, { force: true });
      }
    }
  };
  try {
    for (const { status, path } of files) if (status === "A") unlinkSync(join(repo, path));
    const fromBase = files.filter((f) => f.status !== "A").map((f) => f.path);
    if (fromBase.length) git(repo, "restore", `--source=${base}`, "--worktree", "--", ...fromBase);
  } catch (error) {
    restore();
    throw error;
  }
  return restore;
}

function desktopLifecycle(verb) {
  const result = run("bash", ["scripts/dev-env.sh", verb, "--components", "desktop"], { stdio: ["ignore", 2, 2] });
  if (result.status !== 0) throw new CaptureError(`dev-env.sh ${verb} --components desktop failed.`);
}

function runPrepare(command, state) {
  if (!command) return;
  log(`running --prepare for ${state}`);
  const result = spawnSync(command, {
    cwd: REPO_ROOT,
    shell: true,
    stdio: ["ignore", 2, 2],
    env: { ...process.env, DESKTOP_CAPTURE_STATE: state },
  });
  if (result.status !== 0) throw new CaptureError(`--prepare exited with ${result.status} for ${state}.`);
}

async function compare(options) {
  const base = options.compare;
  try {
    git(REPO_ROOT, "rev-parse", "--verify", "--quiet", `${base}^{commit}`);
  } catch {
    throw new CaptureError(`${base} is not a commit.`);
  }
  const files = changedFiles(REPO_ROOT, base, options.pathspecs);
  if (!files.length) throw new CaptureError(`no tracked file differs from ${base}${options.pathspecs.length ? " in the given paths" : ""}.`);
  managedDesktop();
  mkdirSync(options.out, { recursive: true });
  const settle = options.settle ?? 5;
  const capture = { title: options.title, settle };

  const backupDir = mkdtempSync(join(tmpdir(), "desktop-window-backup-"));
  log(`${files.length} file(s) differ from ${base}; worktree backup in ${backupDir}`);
  let restore = null;
  // Desktop is stopped before each swap so HMR never serves a half-swapped tree.
  let desktopOnWorktree = true;
  const restoreNow = () => {
    if (!restore) return;
    restore();
    restore = null;
    log("worktree restored");
  };
  // Synchronous so signal and exit handlers can run it too. A failed restore
  // throws before the backup is deleted.
  const cleanup = () => {
    restoreNow();
    if (!desktopOnWorktree) {
      desktopOnWorktree = true;
      log("relaunching Desktop on the restored worktree");
      try {
        desktopLifecycle("down");
        desktopLifecycle("up");
      } catch (error) {
        log(`${error.message} Run make up C=desktop to bring it back.`);
      }
    }
    rmSync(backupDir, { recursive: true, force: true });
  };
  const onSignal = (signal) => {
    cleanup();
    process.exit(signal === "SIGINT" ? 130 : 143);
  };
  for (const signal of ["SIGINT", "SIGTERM", "SIGHUP"]) process.on(signal, onSignal);
  process.on("exit", cleanup);

  try {
    desktopOnWorktree = false;
    desktopLifecycle("down");
    restore = swapToBase(REPO_ROOT, base, files, backupDir);
    desktopLifecycle("up");
    runPrepare(options.prepare, "before");
    await captureWindow(join(options.out, "before.png"), capture);

    desktopLifecycle("down");
    restoreNow();
    desktopLifecycle("up");
    desktopOnWorktree = true;
    runPrepare(options.prepare, "after");
    await captureWindow(join(options.out, "after.png"), capture);
  } finally {
    cleanup();
  }
}

async function main() {
  const options = parseCli(process.argv.slice(2));
  if (options.help) {
    console.log(USAGE);
    return;
  }
  if (options.compare) await compare(options);
  else await captureWindow(options.out, { title: options.title, settle: options.settle ?? 0 });
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    console.error(`[desktop-window] ${error instanceof CaptureError ? error.message : error.stack}`);
    process.exit(1);
  });
}
