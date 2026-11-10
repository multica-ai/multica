#!/usr/bin/env node
// Attach Playwright to the managed Desktop app over the Chrome DevTools
// Protocol, so an agent can drive the real Electron renderer with real input.
//
//   make up C=desktop ARGS="--ephemeral --cdp"
//   make env-exec ARGS="-- node scripts/desktop-cdp.mjs --screenshot desktop.png"
//   make env-exec ARGS="-- node scripts/desktop-cdp.mjs --record run.mp4 ./my-steps.mjs"
//
// Run it through `make env-exec`: that is where the CDP endpoint, renderer
// port, backend port and dev verification code come from. A script argument is
// an ES module whose default export receives { browser, context, page, env,
// login, screencast, args }. The managed Desktop starts signed in; --login (or
// the script's `login()`) restores that session after a step signed out.
//
// The helper only ever disconnects. The app belongs to the environment, and
// `make down` / `make destroy` are what stop it.

import { spawnSync } from "node:child_process";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { pathToFileURL } from "node:url";

import { chromium } from "@playwright/test";

const USAGE = `Usage: node scripts/desktop-cdp.mjs [options] [script.mjs [script args...]]

  --login             make sure the dev user is signed in and onboarded, then wait for the shell
  --screenshot FILE   save a PNG of the main window when done
  --record FILE.mp4   record a CDP screencast while the script runs
  --cdp URL           CDP endpoint (default: $MULTICA_DEV_DESKTOP_CDP_URL)
  --timeout SECONDS   how long to wait for the app and the shell (default: 60)

Run through \`make env-exec ARGS="-- node scripts/desktop-cdp.mjs ..."\`.`;

function fail(message) {
  console.error(`[desktop-cdp] ${message}`);
  process.exit(1);
}

function log(message) {
  console.log(`[desktop-cdp] ${message}`);
}

function parseCli(argv) {
  const options = { login: false, screenshot: "", record: "", cdp: "", timeout: 60, script: "", args: [] };
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    const value = () => {
      if (i + 1 >= argv.length) fail(`${arg} needs a value.\n\n${USAGE}`);
      return argv[++i];
    };
    if (arg === "-h" || arg === "--help") {
      console.log(USAGE);
      process.exit(0);
    } else if (arg === "--login") options.login = true;
    else if (arg === "--screenshot") options.screenshot = value();
    else if (arg === "--record") options.record = value();
    else if (arg === "--cdp") options.cdp = value();
    else if (arg === "--timeout") options.timeout = Number(value());
    else if (arg.startsWith("-")) fail(`unknown option ${arg}\n\n${USAGE}`);
    else {
      options.script = arg;
      options.args = argv.slice(i + 1);
      break;
    }
  }
  if (!Number.isFinite(options.timeout) || options.timeout <= 0) fail("--timeout must be a positive number of seconds.");
  return options;
}

const sleep = (ms) => new Promise((done) => setTimeout(done, ms));

async function connect(cdpUrl, deadline) {
  for (;;) {
    try {
      return await chromium.connectOverCDP(cdpUrl);
    } catch (error) {
      if (Date.now() > deadline) {
        fail(`could not attach to ${cdpUrl}: ${error.message}\nIs the desktop component running with a debug port? Check \`make status\`.`);
      }
      await sleep(1000);
    }
  }
}

// The main window and issue windows load the same renderer URL; only the
// preload's window context tells them apart.
async function mainRendererPage(browser, rendererUrl, deadline) {
  const port = new URL(rendererUrl).port;
  for (;;) {
    const pages = browser
      .contexts()
      .flatMap((context) => context.pages())
      .filter((page) => {
        try {
          const url = new URL(page.url());
          return url.protocol === "http:" && url.port === port;
        } catch {
          return false;
        }
      });
    for (const page of pages) {
      const kind = await page
        .evaluate(() => window.desktopAPI?.windowContext?.kind ?? "main")
        .catch(() => "");
      if (kind === "main") return page;
    }
    if (Date.now() > deadline) fail(`no main renderer window on port ${port} appeared in time.`);
    await sleep(500);
  }
}

async function request(apiUrl, method, path, { token, workspaceId, body } = {}) {
  const headers = { "Content-Type": "application/json" };
  if (token) headers.Authorization = `Bearer ${token}`;
  if (workspaceId) headers["X-Workspace-ID"] = workspaceId;
  const response = await fetch(`${apiUrl}${path}`, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await response.text();
  if (!response.ok) {
    const error = new Error(`${method} ${path} → ${response.status} ${text.slice(0, 300)}`);
    error.status = response.status;
    throw error;
  }
  return text ? JSON.parse(text) : null;
}

async function tokenIsValid(apiUrl, token) {
  if (!token) return false;
  try {
    await request(apiUrl, "GET", "/api/me", { token });
    return true;
  } catch {
    return false;
  }
}

// `make up` signs the environment's CLI profile in with a personal access
// token, the same one Desktop starts with. Reusing it avoids requesting a new
// login code, which the backend rate-limits.
function profileToken(env) {
  if (!env.MULTICA_DEV_PROFILE) return null;
  const home = env.MULTICA_DEV_PROFILES_HOME || join(homedir(), ".multica", "profiles");
  try {
    return JSON.parse(readFileSync(join(home, env.MULTICA_DEV_PROFILE, "config.json"), "utf8")).token || null;
  } catch {
    return null;
  }
}

async function ensureWorkspace(apiUrl, token, env) {
  const slug = env.MULTICA_DEV_WORKSPACE_SLUG || "dev";
  const name = env.MULTICA_DEV_WORKSPACE_NAME || "Dev";
  const workspaces = (await request(apiUrl, "GET", "/api/workspaces", { token })) ?? [];
  const existing = workspaces.find((ws) => ws.slug === slug) ?? workspaces[0];
  if (existing) return existing;
  return request(apiUrl, "POST", "/api/workspaces", { token, body: { name, slug } });
}

// Signs in through the API rather than the form: Desktop keeps its session in
// localStorage, so writing the token and reloading lands in the same state a
// real login does, without depending on login-page markup.
async function login(page, env, { timeoutMs }) {
  if (!env.PORT) fail("PORT is not set; run this through `make env-exec`.");
  const apiUrl = `http://localhost:${env.PORT}`;
  const email = env.MULTICA_DEV_EMAIL || "dev@localhost";
  const code = env.MULTICA_DEV_VERIFICATION_CODE || "888888";

  const stored = await page.evaluate(() => localStorage.getItem("multica_token"));
  let token = null;
  for (const candidate of [stored, profileToken(env)]) {
    if (await tokenIsValid(apiUrl, candidate)) {
      token = candidate;
      break;
    }
  }
  if (!token) {
    try {
      await request(apiUrl, "POST", "/auth/send-code", { body: { email } });
    } catch (error) {
      if (error.status !== 429) throw error;
      fail(`${error.message}\nA login code was requested moments ago; wait a minute and retry.`);
    }
    try {
      ({ token } = await request(apiUrl, "POST", "/auth/verify-code", { body: { email, code } }));
    } catch (error) {
      fail(`${error.message}\nDo not retry immediately: repeated verify attempts lock the code. Wait ~40s.`);
    }
  }

  const workspace = await ensureWorkspace(apiUrl, token, env);
  const me = await request(apiUrl, "GET", "/api/me", { token });
  if (!me.onboarded_at) {
    await request(apiUrl, "POST", "/api/me/onboarding/complete", {
      token,
      workspaceId: workspace.id,
      body: { completion_path: "skip_existing", workspace_id: workspace.id },
    });
  }

  // The desktop shell's canvas; it shows a role=status loader until the
  // workspace has resolved.
  const shell = page.locator('[data-slot="main-canvas"]').first();
  const changed = token !== stored || !me.onboarded_at;
  if (changed || !(await shell.isVisible())) {
    await page.evaluate((value) => localStorage.setItem("multica_token", value), token);
    await page.reload();
  }
  await shell.waitFor({ state: "visible", timeout: timeoutMs });
  await shell.locator('> [role="status"]').waitFor({ state: "detached", timeout: timeoutMs });
  log(`signed in as ${email} in workspace ${workspace.slug}`);
  return { token, workspace };
}

// CDP screencasts emit a frame only when the page repaints, so each frame's
// display time comes from its timestamp; the ffconcat file keeps that timing.
async function screencast(page, dir, { quality = 80 } = {}) {
  mkdirSync(dir, { recursive: true });
  const session = await page.context().newCDPSession(page);
  const frames = [];
  session.on("Page.screencastFrame", ({ data, metadata, sessionId }) => {
    const file = `frame-${String(frames.length).padStart(5, "0")}.jpg`;
    frames.push({ file, timestamp: metadata.timestamp });
    writeFileSync(join(dir, file), Buffer.from(data, "base64"));
    session.send("Page.screencastFrameAck", { sessionId }).catch(() => {});
  });
  await session.send("Page.startScreencast", { format: "jpeg", quality, everyNthFrame: 1 });

  return async function stop() {
    const stoppedAt = Date.now() / 1000;
    await session.send("Page.stopScreencast").catch(() => {});
    await session.detach().catch(() => {});
    const lines = ["ffconcat version 1.0"];
    frames.forEach((frame, index) => {
      const next = frames[index + 1]?.timestamp ?? Math.max(stoppedAt, frame.timestamp + 0.5);
      lines.push(`file ${frame.file}`, `duration ${Math.max(next - frame.timestamp, 0.01).toFixed(3)}`);
    });
    if (frames.length) lines.push(`file ${frames.at(-1).file}`);
    const concat = join(dir, "frames.ffconcat");
    writeFileSync(concat, `${lines.join("\n")}\n`);
    return { dir, concat, frames: frames.length };
  };
}

function encodeVideo(concat, output) {
  const args = [
    "-y", "-loglevel", "error", "-f", "concat", "-i", concat,
    "-vf", "pad=ceil(iw/2)*2:ceil(ih/2)*2", "-pix_fmt", "yuv420p", "-vsync", "vfr", output,
  ];
  const result = spawnSync("ffmpeg", args, { stdio: "inherit" });
  if (result.error || result.status !== 0) {
    log(`ffmpeg could not encode the video; frames are kept. Encode with: ffmpeg ${args.join(" ")}`);
    return false;
  }
  return true;
}

async function main() {
  const options = parseCli(process.argv.slice(2));
  const env = process.env;
  const cdpUrl = options.cdp || env.MULTICA_DEV_DESKTOP_CDP_URL;
  if (!cdpUrl) {
    fail('no CDP endpoint. Start Desktop with one (make up C=desktop ARGS="--cdp") and run this through `make env-exec`.');
  }
  const rendererUrl = env.MULTICA_DEV_DESKTOP_RENDERER_URL;
  if (!rendererUrl) fail("MULTICA_DEV_DESKTOP_RENDERER_URL is not set; run this through `make env-exec`.");

  const timeoutMs = options.timeout * 1000;
  const deadline = Date.now() + timeoutMs;
  const browser = await connect(cdpUrl, deadline);
  const page = await mainRendererPage(browser, rendererUrl, deadline);
  const context = page.context();
  log(`attached to ${cdpUrl} → ${page.url()}`);

  const loginHere = () => login(page, env, { timeoutMs });
  if (options.login) await loginHere();

  let stopRecording;
  const recordTo = options.record ? resolve(options.record) : "";
  if (recordTo) stopRecording = await screencast(page, `${recordTo}.frames`);

  try {
    if (options.script) {
      const module = await import(pathToFileURL(resolve(options.script)).href);
      if (typeof module.default !== "function") fail(`${options.script} must export a default async function.`);
      await module.default({ browser, context, page, env, login: loginHere, screencast, args: options.args });
    }
  } finally {
    if (stopRecording) {
      const { concat, frames } = await stopRecording();
      if (frames === 0) log("the screencast captured no frames (nothing repainted).");
      else if (encodeVideo(concat, recordTo)) log(`recorded ${frames} frames → ${recordTo}`);
    }
  }

  if (options.screenshot) {
    const file = resolve(options.screenshot);
    mkdirSync(dirname(file), { recursive: true });
    await page.screenshot({ path: file });
    log(`screenshot → ${file}`);
  }
}

main().then(
  // Exiting drops the CDP connection without asking the app to close.
  () => process.exit(0),
  (error) => fail(error?.stack || String(error)),
);
