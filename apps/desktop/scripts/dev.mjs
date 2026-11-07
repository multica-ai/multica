#!/usr/bin/env node
// Dev launcher for `pnpm dev:desktop`.
//
// Derives per-worktree isolation env (renderer port + app name) so multiple
// worktrees can run `pnpm dev:desktop` side-by-side, then runs the same chain
// as before — bundle the CLI, brand the dev Electron, start electron-vite —
// inheriting the augmented env. A plain `&&` chain in package.json can't do
// this: each `&&` step is its own process, so an env tweak in step 1 wouldn't
// reach electron-vite in step 3. Args (e.g. `--mode staging`) pass through to
// electron-vite.

import { spawnSync } from "node:child_process";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { envWithLocalBins } from "./package.mjs";
import { withClaudeLoginEnv } from "./claude-login-env.mjs";
import {
  applyCloudDevEnv,
  applyWorktreeDevEnv,
  repoRootFromScriptDir,
} from "./worktree-dev-env.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const desktopRoot = dirname(here);
const args = process.argv.slice(2);
const cloud = args.includes("--cloud");
const claudeLogin = args.includes("--claude-login");
const devEnv = claudeLogin ? withClaudeLoginEnv(process.env) : process.env;
if (claudeLogin) {
  console.log("[dev:desktop] Inherited Claude provider overrides cleared; using local CLI settings and login.");
}
if (cloud && args.some((arg) => arg === "--mode" || arg.startsWith("--mode="))) {
  throw new Error("Cloud development selects its own production mode; omit --mode.");
}

applyWorktreeDevEnv(devEnv, {
  root: repoRootFromScriptDir(here),
  log: true,
});
if (cloud) {
  applyCloudDevEnv(devEnv, { root: repoRootFromScriptDir(here) });
  console.log(`[dev:desktop] Multica Cloud → ${devEnv.VITE_API_URL}, app ${devEnv.DESKTOP_APP_SUFFIX}, renderer :${devEnv.DESKTOP_RENDERER_PORT}`);
}

function run(command, args, { shell = false, env = devEnv } = {}) {
  const result = spawnSync(command, args, {
    stdio: "inherit",
    env,
    shell,
    cwd: desktopRoot,
  });
  if (result.error) {
    console.error(`[dev:desktop] failed to run ${command}: ${result.error.message}`);
    process.exit(1);
  }
  if (result.status !== 0) process.exit(result.status ?? 1);
}

const node = process.execPath;
run(node, [join(here, "bundle-cli.mjs"), "--require-build"]);
run(node, [join(here, "brand-dev-electron.mjs")]);
run(node, [join(here, "stage-cua-driver.mjs")]);

const isWin = process.platform === "win32";
// electron-vite's bin lands in apps/desktop/node_modules/.bin under the
// isolated linker but only in the repo-root .bin under the hoisted linker
// (.npmrc node-linker=hoisted); envWithLocalBins puts both on PATH.
run("electron-vite", ["dev", ...(cloud ? ["--mode", "production"] : []), ...args.filter((arg) => arg !== "--cloud" && arg !== "--claude-login")], {
  shell: isWin,
  env: envWithLocalBins(devEnv),
});
